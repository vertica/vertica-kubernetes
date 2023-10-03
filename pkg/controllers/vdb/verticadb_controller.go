/*
 (c) Copyright [2021-2023] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package vdb

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vlog"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
)

// VerticaDBReconciler reconciles a VerticaDB object
type VerticaDBReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Cfg    *rest.Config
	EVRec  record.EventRecorder
	OpCfg  opcfg.OperatorConfig
}

//+kubebuilder:rbac:groups=vertica.com,resources=verticadbs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,resources=verticadbs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,resources=verticadbs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=mutatingwebhookconfigurations,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingwebhookconfigurations,verbs=get;list;watch;update;patch

// We need the ability to update CRDs so that we can refresh the client cert for
// the conversion webhook.
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;update;patch

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaDBReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.VerticaDB{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *VerticaDBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("verticadb", req.NamespacedName)
	log.Info("starting reconcile of VerticaDB")

	vdb := &vapi.VerticaDB{}
	err := r.Get(ctx, req.NamespacedName, vdb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Remove any metrics for the vdb that we found to be deleted
			metrics.HandleVDBDelete(req.NamespacedName.Namespace, req.NamespacedName.Name, log)
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaDB resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaDB")
		return ctrl.Result{}, err
	}

	if vmeta.IsPauseAnnotationSet(vdb.Annotations) {
		log.Info(fmt.Sprintf("The pause annotation %s is set. Suspending the iteration", vmeta.PauseOperatorAnnotation),
			"result", ctrl.Result{}, "err", nil)
		return ctrl.Result{}, nil
	}

	passwd, err := r.GetSuperuserPassword(ctx, vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	prunner := cmds.MakeClusterPodRunner(log, r.Cfg, passwd)
	// We use the same pod facts for all reconcilers. This allows to reuse as
	// much as we can. Some reconcilers will purposely invalidate the facts if
	// it is known they did something to make them stale.
	pfacts := MakePodFacts(r, prunner)
	dispatcher := r.makeDispatcher(log, vdb, prunner, passwd)
	var res ctrl.Result

	// Iterate over each actor
	actors := r.constructActors(log, vdb, prunner, &pfacts, dispatcher)
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			// Handle requeue time priority.
			// If any function needs a requeue and we have a RequeueTime set,
			// then overwrite RequeueAfter.
			// Functions such as Upgrade may already set RequeueAfter and Requeue to false
			if (res.Requeue || res.RequeueAfter > 0) && vdb.Spec.RequeueTime > 0 {
				res.Requeue = false
				res.RequeueAfter = time.Second * time.Duration(vdb.Spec.RequeueTime)
			}
			log.Info("aborting reconcile of VerticaDB", "result", res, "err", err)
			return res, err
		}
	}

	log.Info("ending reconcile of VerticaDB", "result", res, "err", err)
	return res, err
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *VerticaDBReconciler) constructActors(log logr.Logger, vdb *vapi.VerticaDB, prunner *cmds.ClusterPodRunner,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a vdb.
	// Note, we run the StatusReconciler multiple times. This allows us to
	// refresh the status of the vdb as we do operations that affect it.
	return []controllers.ReconcileActor{
		// Always start with a status reconcile in case the prior reconcile failed.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		// Report any pods that have low disk space
		MakeLocalDataCheckReconciler(r, vdb, pfacts),
		// Handle upgrade actions for any k8s objects created in prior versions
		// of the operator.
		MakeUpgradeOperator120Reconciler(r, log, vdb),
		// Create a TLS secret for the NMA service
		MakeHTTPServerCertGenReconciler(r, log, vdb),
		// Create ServiceAcount, Role and RoleBindings needed for vertica pods
		MakeServiceAccountReconciler(r, log, vdb),
		// Update any k8s objects with some exceptions. For instance, preserve
		// scaling. This is needed *before* upgrade and restart in case a change
		// was made with the image change that would prevent the pods from
		// running. An example of this is if we also changed a volume mount
		// (i.e. renamed a ConfigMap). We want the objects to reflect the new
		// volume mount so that we can start the pod.  Similar rationale for
		// preserving other things.
		MakeObjReconciler(r, log, vdb, pfacts,
			ObjReconcileModePreserveScaling|ObjReconcileModePreserveUpdateStrategy),
		// Add annotations/labels to each pod about the host running them
		MakeAnnotateAndLabelPodReconciler(r, log, vdb, pfacts),
		// Handles vertica server upgrade (i.e., when spec.image changes)
		MakeOfflineUpgradeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeOnlineUpgradeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		// Stop vertica if the status condition indicates
		MakeStopDBReconciler(r, vdb, prunner, pfacts, dispatcher),
		// Handles restart + re_ip of vertica
		MakeRestartReconciler(r, log, vdb, prunner, pfacts, true, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Ensure we add labels to any pod rescheduled so that Service objects route traffic to it.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, PodRescheduleApplyMethod, ""),
		// Remove Service label for any pods that are pending delete.  This will
		// cause the Service object to stop routing traffic to them.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, DelNodeApplyMethod, ""),
		// Wait for any nodes that are pending delete with active connections to leave.
		MakeDrainNodeReconciler(r, vdb, prunner, pfacts),
		// Handles calls to remove subcluster from vertica catalog
		MakeDBRemoveSubclusterReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handles calls to remove a database node from the cluster
		MakeDBRemoveNodeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to remove hosts from admintools.conf
		MakeUninstallReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Creates or updates any k8s objects the CRD creates. This includes any
		// statefulsets and service objects.
		MakeObjReconciler(r, log, vdb, pfacts, ObjReconcileModeAll),
		// Set version info in the annotations and check that it is the minimum
		MakeVersionReconciler(r, log, vdb, prunner, pfacts, false),
		// Handle calls to add hosts to admintools.conf
		MakeInstallReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to create a database
		MakeCreateDBReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		// Handle calls to revive a database
		MakeReviveDBReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		// Create and revive are mutually exclusive exclusive, so this handles
		// status updates after both of them.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Update the labels in pods so that Services route to nodes to them.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, PodRescheduleApplyMethod, ""),
		// Handle calls to add new subcluster to the catalog
		MakeDBAddSubclusterReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to add a new database node to the cluster
		MakeDBAddNodeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to rebalance_shards
		MakeRebalanceShardsReconciler(r, log, vdb, prunner, pfacts, "" /* all subclusters */),
		// Update the label in pods so that Service routing uses them if they
		// have finished being rebalanced.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, AddNodeApplyMethod, ""),
		// Resize any PVs if the local data size changed in the vdb
		MakeResizePVReconciler(r, log, vdb, prunner, pfacts),
	}
}

// GetSuperuserPassword returns the superuser password if it has been provided
func (r *VerticaDBReconciler) GetSuperuserPassword(ctx context.Context, vdb *vapi.VerticaDB) (string, error) {
	if vdb.Spec.SuperuserPasswordSecret == "" {
		return "", nil
	}

	if vmeta.UseGCPSecretManager(vdb.Annotations) {
		secretCnts, err := cloud.ReadFromGSM(ctx, vdb.Spec.SuperuserPasswordSecret)
		if err != nil {
			return "", fmt.Errorf("failed to read superuser password from GSM: %w", err)
		}
		pwd, ok := secretCnts[builder.SuperuserPasswordKey]
		if !ok {
			return "", fmt.Errorf("password not found, secret must have a key with name '%s'", builder.SuperuserPasswordKey)
		}
		return pwd, nil
	}

	secret := &corev1.Secret{}
	secretName := names.GenSUPasswdSecretName(vdb)
	err := r.Get(ctx, secretName, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			r.EVRec.Eventf(vdb, corev1.EventTypeWarning, events.SuperuserPasswordSecretNotFound,
				"Secret for superuser password '%s' was not found", secretName.Name)
		}
		return "", err
	}
	pwd, ok := secret.Data[builder.SuperuserPasswordKey]
	if !ok {
		return "", fmt.Errorf("password not found, secret must have a key with name '%s'", builder.SuperuserPasswordKey)
	}
	return string(pwd), nil
}

// checkShardToNodeRatio will check the subclusters ratio of shards to node.  If
// it is outside the bounds of optimal value then an event is written to inform
// the user.
func (r *VerticaDBReconciler) checkShardToNodeRatio(vdb *vapi.VerticaDB, sc *vapi.Subcluster) {
	// If ksafety is 0, this is a toy database since we cannot grow beyond 3
	// nodes.  Don't bother logging anything in that case.
	if vdb.Spec.KSafety == vapi.KSafety0 {
		return
	}
	ratio := float32(vdb.Spec.ShardCount) / float32(sc.Size)
	const SuboptimalRatio = float32(3.0)
	if ratio > SuboptimalRatio {
		r.Eventf(vdb, corev1.EventTypeWarning, events.SuboptimalNodeCount,
			"Subcluster '%s' has a suboptimal node count.  Consider increasing its size so that the shard to node ratio is %d:1 or less.",
			sc.Name, int(SuboptimalRatio))
	}
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (r *VerticaDBReconciler) makeDispatcher(log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	passwd string) vadmin.Dispatcher {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		vcc := vops.VClusterCommands{
			Log: vlog.Printer{
				Log:           log.WithName("vcluster"),
				LogToFileOnly: false,
			},
		}
		return vadmin.MakeVClusterOps(log, vdb, r.Client, &vcc, passwd, r.EVRec)
	}
	return vadmin.MakeAdmintools(log, vdb, prunner, r.EVRec, r.OpCfg.DevMode)
}

// Event a wrapper for Event() that also writes a log entry
func (r *VerticaDBReconciler) Event(vdb runtime.Object, eventtype, reason, message string) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Event(vdb, eventtype, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *VerticaDBReconciler) Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(vdb, eventtype, reason, messageFmt, args...)
}
