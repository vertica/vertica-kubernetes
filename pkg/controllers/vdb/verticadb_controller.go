/*
 (c) Copyright [2021-2024] Open Text.
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
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"github.com/google/uuid"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
)

// VerticaDBReconciler reconciles a VerticaDB object
type VerticaDBReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	Cfg                *rest.Config
	EVRec              record.EventRecorder
	Namespace          string
	MaxBackOffDuration int
	CacheManager       cache.CacheManager
}

// +kubebuilder:rbac:groups=vertica.com,resources=verticadbs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=vertica.com,resources=verticadbs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=vertica.com,resources=verticadbs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;create
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups="",resources=pods/status,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;update;delete;create
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;list;watch;create;update

// SetupWithManager sets up the controller with the Manager.
//
//nolint:gocritic
func (r *VerticaDBReconciler) SetupWithManager(mgr ctrl.Manager, options controller.Options) error {
	ctrlManager := ctrl.NewControllerManagedBy(mgr).
		WithOptions(options).
		For(&vapi.VerticaDB{}).
		Owns(&corev1.ServiceAccount{}).
		Owns(&rbacv1.Role{}).
		Owns(&rbacv1.RoleBinding{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&appsv1.Deployment{})

	discoveryClient := discovery.NewDiscoveryClientForConfigOrDie(mgr.GetConfig())
	if isServiceMonitorObjectInstalled(discoveryClient) {
		ctrlManager.Owns(&monitoringv1.ServiceMonitor{})
	}

	ctrlManager.WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
		if r.Namespace == "" {
			return true
		}
		return obj.GetNamespace() == r.Namespace
	}))

	return ctrlManager.Complete(r)
}

// Function to check if servicemonitor CRD exists
func isServiceMonitorObjectInstalled(discoveryClient discovery.DiscoveryInterface) bool {
	gvr := schema.GroupVersionResource{Group: "monitoring.coreos.com", Version: "v1", Resource: "servicemonitors"}
	_, err := discoveryClient.ServerResourcesForGroupVersion(gvr.GroupVersion().String())
	return err == nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *VerticaDBReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	// Generate a unique uuid for each reconcile iteration so we can easily
	// trace actions within a reconcile.
	reconcileUUID := uuid.New()
	log := r.Log.WithValues("verticadb", req.NamespacedName, "reconcile-uuid", reconcileUUID)
	log.Info("starting reconcile of VerticaDB")

	vdb := &vapi.VerticaDB{}
	err := r.Get(ctx, req.NamespacedName, vdb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Remove any metrics for the vdb that we found to be deleted
			metrics.HandleVDBDelete(req.NamespacedName.Namespace, req.NamespacedName.Name, log)
			r.CacheManager.DestroyCertCacheForVdb(req.NamespacedName.Namespace, req.NamespacedName.Name)
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaDB resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaDB")
		return ctrl.Result{}, err
	}
	log.Info("VerticaDB details", "uid", vdb.UID, "resourceVersion", vdb.ResourceVersion,
		"vclusterOps", vmeta.UseVClusterOps(vdb.Annotations), "user", vdb.GetVerticaUser(),
		"tls cert rotate enabled", vmeta.UseTLSAuth(vdb.Annotations))
	if vmeta.IsPauseAnnotationSet(vdb.Annotations) {
		log.Info(fmt.Sprintf("The pause annotation %s is set. Suspending the iteration", vmeta.PauseOperatorAnnotation),
			"result", ctrl.Result{}, "err", nil)
		return ctrl.Result{}, nil
	}

	passwd, err := r.GetSuperuserPassword(ctx, log, vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	prunner := cmds.MakeClusterPodRunner(log, r.Cfg, vdb.GetVerticaUser(), passwd, vmeta.UseTLSAuth(vdb.Annotations))
	// We use the same pod facts for all reconcilers. This allows to reuse as
	// much as we can. Some reconcilers will purposely invalidate the facts if
	// it is known they did something to make them stale.
	pfacts := podfacts.MakePodFactsWithCacheManager(r, prunner, log, passwd, r.CacheManager)
	dispatcher := r.makeDispatcher(log, vdb, prunner, passwd)
	var res ctrl.Result

	r.InitCertCacheForVdb(vdb)
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
			if (res.Requeue || res.RequeueAfter > 0) && vdb.GetRequeueTime() > 0 {
				res.Requeue = false
				res.RequeueAfter = time.Second * time.Duration(vdb.GetRequeueTime())
			}
			log.Info("aborting reconcile of VerticaDB", "result", res, "err", err)
			return res, err
		}
	}
	r.CleanCacheForVdb(vdb)
	log.Info("ending reconcile of VerticaDB", "result", res, "err", err)
	return res, err
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
//
//nolint:funlen
func (r *VerticaDBReconciler) constructActors(log logr.Logger, vdb *vapi.VerticaDB, prunner *cmds.ClusterPodRunner,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a vdb.
	// Note, we run the StatusReconciler multiple times. This allows us to
	// refresh the status of the vdb as we do operations that affect it.
	return []controllers.ReconcileActor{
		// Log an event if we are in a crash loop due to a bad deployment type
		// chosen. This should be at or near the top as it will help with error
		// detection when we can't even run anything in the pod. So any
		// reconcile actor that depends on running pods should not be before
		// this one.
		MakeCrashLoopReconciler(r, log, vdb),
		// Modify or record the annotations in the vdb so later reconcilers can
		// get the correct information.
		MakeObjReconciler(r, log, vdb, pfacts, ObjReconcileModeAnnotation),
		// Validate the vdb after operator upgraded
		MakeValidateVDBReconciler(r, log, vdb),
		// Always generate cert first if nothing is provided
		MakeTLSServerCertGenReconciler(r, log, vdb),
		// Set up configmap which stores env variables for NMA container
		MakeNMACertConfigMapReconciler(r, log, vdb),
		// Trigger sandbox upgrade when the image field for the sandbox
		// is changed
		MakeSandboxUpgradeReconciler(r, log, vdb, false),
		// Update the sandbox/subclusters' shutdown field to match the value of
		// the spec.
		MakeShutdownSpecReconciler(r, vdb),
		// Update sandbox subcluster type in db according to its type in vdb spec
		MakeAlterSandboxTypeReconciler(r, log, vdb, pfacts),
		// Update the vertica image for unsandboxed subclusters
		MakeUnsandboxImageVersionReconciler(r, vdb, log, pfacts),
		// Always start with a status reconcile in case the prior reconcile failed.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		// Report any pods that have low disk space
		MakeLocalDataCheckReconciler(r, vdb, pfacts),
		// Handle upgrade actions for any k8s objects created in prior versions
		// of the operator.
		MakeUpgradeOperatorReconciler(r, log, vdb),
		// Create ServiceAcount, Role and RoleBindings needed for vertica pods
		MakeServiceAccountReconciler(r, log, vdb),
		// Handle setting up the pod security context. This picks the
		// UID/fsGroup that we will run with.
		MakePodSecurityReconciler(r, log, vdb),
		// Update any k8s objects with some exceptions. For instance, preserve
		// scaling. This is needed *before* upgrade and restart in case a change
		// was made with the image change that would prevent the pods from
		// running. An example of this is if we also changed a volume mount
		// (i.e. renamed a ConfigMap). We want the objects to reflect the new
		// volume mount so that we can start the pod.  Similar rationale for
		// preserving other things.
		MakeObjReconciler(r, log, vdb, pfacts,
			ObjReconcileModePreserveScaling|ObjReconcileModePreserveUpdateStrategy),
		// Set up TLS config if users turn it on
		MakeTLSReconciler(r, log, vdb, prunner, dispatcher, pfacts),
		// Update the service monitor that will allow prometheus to scrape the
		// metrics from the vertica pods.
		MakeServiceMonitorReconciler(vdb, r, log),
		// Add annotations/labels to each pod about the host running them
		MakeAnnotateAndLabelPodReconciler(r, log, vdb, pfacts),
		// Trigger sandbox shutdown when the shutdown field of the sandbox
		// is changed
		MakeSandboxShutdownReconciler(r, log, vdb, true),
		// Handles vertica server upgrade (i.e., when spec.image changes)
		MakeOfflineUpgradeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeReadOnlyOnlineUpgradeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeOnlineUpgradeReconciler(r, log, vdb, pfacts, dispatcher),
		// Stop vertica if the status condition indicates
		MakeStopDBReconciler(r, vdb, prunner, pfacts, dispatcher),
		// Stop subclusters that have shutdown set to true.
		MakeSubclusterShutdownReconciler(r, log, vdb, dispatcher, pfacts),
		// Check the version information ahead of restart. The version is needed
		// to properly pick the correct NMA deployment (monolithic vs sidecar).
		MakeImageVersionReconciler(r, log, vdb, prunner, pfacts, false /* enforceUpgradePath */, nil, false),
		// Handles restart + re_ip of vertica
		MakeRestartReconciler(r, log, vdb, prunner, pfacts, true, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconcilerWithShutdown(r.Client, r.Scheme, log, vdb, pfacts),
		// Ensure we add labels to any pod rescheduled so that Service objects route traffic to it.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, PodRescheduleApplyMethod, ""),
		// Remove Service label for any pods that are pending delete.  This will
		// cause the Service object to stop routing traffic to them.
		MakeClientRoutingLabelReconciler(r, log, vdb, pfacts, DelNodeApplyMethod, ""),
		// Wait for any nodes that are pending delete with active connections to leave.
		MakeDrainNodeReconciler(r, vdb, prunner, pfacts),
		// Handles calls to remove subcluster from vertica catalog
		MakeDBRemoveSubclusterReconciler(r, log, vdb, prunner, pfacts, dispatcher, false),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handles calls to remove a database node from the cluster
		MakeDBRemoveNodeReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeMetricReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to remove hosts from admintools.conf
		MakeUninstallReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Set version info in the annotations and check that the deployment is
		// compatible with the image.
		MakeImageVersionReconciler(r, log, vdb, prunner, pfacts, false /* enforceUpgradePath */, nil, false),
		// Creates or updates any k8s objects the CRD creates. This includes any
		// statefulsets and service objects.
		MakeObjReconciler(r, log, vdb, pfacts, ObjReconcileModeAll),
		// Handle calls to add hosts to admintools.conf
		MakeInstallReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to create a database
		MakeCreateDBReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		// Handle calls to revive a database
		MakeReviveDBReconciler(r, log, vdb, prunner, pfacts, dispatcher),
		MakeTLSReconciler(r, log, vdb, prunner, dispatcher, pfacts),
		// Update the service monitor that will allow prometheus to scrape the
		// metrics from the vertica pods.
		MakeServiceMonitorReconciler(vdb, r, log),
		// Add additional buckets for data replication
		MakeAddtionalBucketsReconciler(r, log, vdb, prunner, pfacts),
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
		// Update subcluster type in db according to its type in vdb spec
		MakeAlterSubclusterTypeReconciler(r, log, vdb, pfacts, dispatcher, nil /* configMap */),
		// Handle calls to add subclusters to sandboxes
		MakeSandboxSubclusterReconciler(r, log, vdb, pfacts, dispatcher, r.Client, false),
		// Handle calls to move subclusters from sandboxes to main cluster
		MakeUnsandboxSubclusterReconciler(r, log, vdb, r.Client),
		// Update the status subcluster types
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Trigger sandbox upgrade when the image field for the sandbox
		// is changed
		MakeSandboxUpgradeReconciler(r, log, vdb, true),
		// Update the sandbox/subclusters' shutdown field to match the value of
		// the spec.
		MakeShutdownSpecReconciler(r, vdb),
		// Trigger sandbox shutdown when the shutdown field of the sandbox
		// is changed
		MakeSandboxShutdownReconciler(r, log, vdb, false),
		// Add the label after update the sandbox subcluster status field
		MakeObjReconciler(r, log, vdb, pfacts, ObjReconcileModeAll),
		// Update sandbox subcluster type in db according to its type in vdb spec
		MakeAlterSandboxTypeReconciler(r, log, vdb, pfacts),
		// Handle calls to create a restore point
		MakeSaveRestorePointReconciler(r, vdb, log, pfacts, dispatcher, r.Client),
		// Resize any PVs if the local data size changed in the vdb
		MakeResizePVReconciler(r, log, vdb, prunner, pfacts),
		// This must be the last reconciler. It makes sure that all dependent
		// objects that the operator creates exist. This is needed encase they
		// are removed in the middle of a reconcile iteration.
		MakeDepObjCheckReconciler(r, log, vdb),
	}
}

// GetSuperuserPassword returns the superuser password if it has been provided
func (r *VerticaDBReconciler) GetSuperuserPassword(ctx context.Context, log logr.Logger, vdb *vapi.VerticaDB) (string, error) {
	return vk8s.GetSuperuserPassword(ctx, r.Client, log, r, vdb)
}

// checkShardToNodeRatio will check the subclusters ratio of shards to node.  If
// it is outside the bounds of optimal value then an event is written to inform
// the user.
func (r *VerticaDBReconciler) checkShardToNodeRatio(vdb *vapi.VerticaDB, sc *vapi.Subcluster) {
	// If ksafety is 0, this is a toy database since we cannot grow beyond 3
	// nodes.  Don't bother logging anything in that case.
	if vdb.IsKSafety0() {
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
		return vadmin.MakeVClusterOps(log, vdb, r.Client, passwd, r.EVRec, vadmin.SetupVClusterOps, r.CacheManager)
	}
	return vadmin.MakeAdmintools(log, vdb, prunner, r.EVRec)
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

// GetClient gives access to the Kubernetes client
func (r *VerticaDBReconciler) GetClient() client.Client {
	return r.Client
}

// GetEventRecorder gives access to the event recorder
func (r *VerticaDBReconciler) GetEventRecorder() record.EventRecorder {
	return r.EVRec
}

// GetConfig gives access to *rest.Config
func (r *VerticaDBReconciler) GetConfig() *rest.Config {
	return r.Cfg
}

func (r *VerticaDBReconciler) InitCertCacheForVdb(vdb *vapi.VerticaDB) {
	fetcher := &cloud.SecretFetcher{
		Client:   r.Client,
		Log:      r.Log,
		Obj:      vdb,
		EVWriter: r.EVRec,
	}
	r.CacheManager.InitCertCacheForVdb(vdb, fetcher)
}

func (r *VerticaDBReconciler) CleanCacheForVdb(vdb *vapi.VerticaDB) {
	certCache := r.CacheManager.GetCertCacheForVdb(vdb.Namespace, vdb.Name)
	certsInUse := []string{
		vdb.Spec.HTTPSNMATLS.Secret,
	}
	if vdb.Spec.ClientServerTLS.Secret != "" {
		certsInUse = append(certsInUse, vdb.Spec.ClientServerTLS.Secret)
	}
	for _, tlsConfig := range vdb.Status.TLSConfigs {
		certsInUse = append(certsInUse, tlsConfig.Secret)
	}
	certCache.CleanCacheForVdb(certsInUse)
}
