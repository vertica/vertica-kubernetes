/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
)

// VerticaDBReconciler reconciles a VerticaDB object
type VerticaDBReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Cfg    *rest.Config
	EVRec  record.EventRecorder
}

//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,namespace=WATCH_NAMESPACE,resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps,namespace=WATCH_NAMESPACE,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",namespace=WATCH_NAMESPACE,resources=pods,verbs=get;list;watch;create;update;delete
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

	// The actors that will be applied, in sequence, to reconcile a vdb.
	// Note, we run the StatusReconciler multiple times. This allows us to
	// refresh the status of the vdb as we do operations that affect it.
	actors := []ReconcileActor{
		// Always start with a status reconcile in case the prior reconcile failed.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handles restart + re_ip of vertica
		MakeRestartReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handles calls to admintools -t db_remove_subcluster
		MakeDBRemoveSubclusterReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handles calls to admintools -t db_remove_node
		MakeDBRemoveNodeReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handle calls to update_vertica --remove-hosts
		MakeUninstallReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Creates or updates any k8s objects the CRD creates. This includes any
		// statefulsets and service objects.
		MakeObjReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Set version info in the annotations and check that it is the minimum
		MakeVersionReconciler(r, log, vdb, prunner, &pfacts),
		// Handle calls to update_vertica --add-hosts
		MakeInstallReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handle calls to admintools -t create_db
		MakeCreateDBReconciler(r, log, vdb, prunner, &pfacts),
		// Handle calls to admintools -t revive_db
		MakeReviveDBReconciler(r, log, vdb, prunner, &pfacts),
		// Create and revive are mutually exclusive exclusive, so this handles
		// status updates after both of them.
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handle calls to admintools -t db_add_subcluster
		MakeDBAddSubclusterReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
		// Handle calls to admintools -t db_add_node
		MakeDBAddNodeReconciler(r, log, vdb, prunner, &pfacts),
		MakeStatusReconciler(r.Client, r.Scheme, log, vdb, &pfacts),
	}

	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if err != nil || res.Requeue {
			if res.Requeue && vdb.Spec.RequeueTime > 0 {
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
	pwd, ok := secret.Data[SuperuserPasswordKey]
	if ok {
		passwd = string(pwd)
	} else {
		log.Error(err, fmt.Sprintf("password not found, secret must have a key with name '%s'", SuperuserPasswordKey))
	}
	return passwd, nil
}
