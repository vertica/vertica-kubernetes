/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
)

// VerticaDBReconciler reconciles a VerticaDB object
type VerticaDBReconciler struct {
	client.Client
	Log                logr.Logger
	Scheme             *runtime.Scheme
	Cfg                *rest.Config
	EVRec              record.EventRecorder
	ServiceAccountName string
}

//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,namespace=WATCH_NAMESPACE,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=WATCH_NAMESPACE,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=WATCH_NAMESPACE,resources=pods,verbs=get;list;watch;create;update;delete;patch
// +kubebuilder:rbac:groups="",namespace=WATCH_NAMESPACE,resources=pods/exec,verbs=create
// +kubebuilder:rbac:groups=core,namespace=WATCH_NAMESPACE,resources=secrets,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaDBReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.VerticaDB{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Complete(r)
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VerticaDB object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
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
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaDB resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaDB")
		return ctrl.Result{}, err
	}

	passwd, err := r.GetSuperuserPassword(ctx, vdb, log)
	if err != nil {
		return ctrl.Result{}, err
	}
	prunner := cmds.MakeClusterPodRunner(log, r.Cfg, passwd)
	// We use the same pod facts for all reconcilers. This allows to reuse as
	// much as we can. Some reconcilers will purposely invalidate the facts if
	// it is known they did something to make them stale.
	pfacts := MakePodFacts(r.Client, prunner)
	var res ctrl.Result

	// Iterate over each actor
	actors := r.constructActors(log, vdb, prunner, &pfacts)
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
				res.RequeueAfter = time.Duration(vdb.Spec.RequeueTime)
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
	pfacts *PodFacts) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a vdb.
	// Note, we run the StatusReconciler multiple times. This allows us to
	// refresh the status of the vdb as we do operations that affect it.
	return []controllers.ReconcileActor{
		// Always start with a status reconcile in case the prior reconcile failed.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle upgrade actions for any k8s objects created in prior versions
		// of the operator.
		MakeUpgradeOperator120Reconciler(r, log, vdb),
		// Handles vertica server upgrade (i.e., when spec.image changes)
		MakeOfflineUpgradeReconciler(r, log, vdb, prunner, pfacts),
		MakeOnlineUpgradeReconciler(r, log, vdb, prunner, pfacts),
		// Creates any missing k8s objects.  This doesn't update existing
		// objects.  It is a special case for when restart is needed but the
		// pods are missing.  We don't want to apply all updates as we may need
		// to go through necessary admintools commands to handle a scale down.
		MakeObjReconciler(r, log, vdb, pfacts, ObjReconcileModeIfNotFound),
		// Handles restart + re_ip of vertica
		MakeRestartReconciler(r, log, vdb, prunner, pfacts, true),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Ensure we add labels to any pod rescheduled so that Service objects route traffic to it.
		MakeClientRoutingLabelReconciler(r, vdb, pfacts, PodRescheduleApplyMethod, ""),
		// Remove Service label for any pods that are pending delete.  This will
		// cause the Service object to stop routing traffic to them.
		MakeClientRoutingLabelReconciler(r, vdb, pfacts, DelNodeApplyMethod, ""),
		// Wait for any nodes that are pending delete with active connections to leave.
		MakeDrainNodeReconciler(r, vdb, prunner, pfacts),
		// Handles calls to admintools -t db_remove_subcluster
		MakeDBRemoveSubclusterReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handles calls to admintools -t db_remove_node
		MakeDBRemoveNodeReconciler(r, log, vdb, prunner, pfacts),
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
		// Handle calls to admintools -t create_db
		MakeCreateDBReconciler(r, log, vdb, prunner, pfacts),
		// Handle calls to admintools -t revive_db
		MakeReviveDBReconciler(r, log, vdb, prunner, pfacts),
		// Create and revive are mutually exclusive exclusive, so this handles
		// status updates after both of them.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Update the labels in pods so that Services route to nodes to them.
		MakeClientRoutingLabelReconciler(r, vdb, pfacts, AddNodeApplyMethod, ""),
		// Handle calls to admintools -t db_add_subcluster
		MakeDBAddSubclusterReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to admintools -t db_add_node
		MakeDBAddNodeReconciler(r, log, vdb, prunner, pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, pfacts),
		// Handle calls to rebalance_shards
		MakeRebalanceShardsReconciler(r, log, vdb, prunner, pfacts, "" /* all subclusters */),
		// Update the label in pods so that Service routing uses them if they
		// have finished being rebalanced.
		MakeClientRoutingLabelReconciler(r, vdb, pfacts, AddNodeApplyMethod, ""),
	}
}

// GetSuperuserPassword returns the superuser password if it has been provided
func (r *VerticaDBReconciler) GetSuperuserPassword(ctx context.Context, vdb *vapi.VerticaDB, log logr.Logger) (string, error) {
	secret := &corev1.Secret{}
	passwd := ""
	secretName := names.GenSUPasswdSecretName(vdb)
	if secretName.Name == "" {
		return passwd, nil
	}
	err := r.Get(ctx, secretName, secret)
	if err != nil {
		if errors.IsNotFound(err) {
			r.EVRec.Eventf(vdb, corev1.EventTypeWarning, events.SuperuserPasswordSecretNotFound,
				"Secret for superuser password '%s' was not found", secretName.Name)
		}
		return passwd, err
	}
	pwd, ok := secret.Data[builder.SuperuserPasswordKey]
	if ok {
		passwd = string(pwd)
	} else {
		log.Error(err, fmt.Sprintf("password not found, secret must have a key with name '%s'", builder.SuperuserPasswordKey))
	}
	return passwd, nil
}
