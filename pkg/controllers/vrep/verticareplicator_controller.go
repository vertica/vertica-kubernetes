/*
Copyright [2021-2024] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vrep

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
)

// VerticaReplicatorReconciler reconciles a VerticaReplicator object
type VerticaReplicatorReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	Cfg    *rest.Config
	EVRec  record.EventRecorder
}

//+kubebuilder:rbac:groups=vertica.com,resources=verticareplicators,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,resources=verticareplicators/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,resources=verticareplicators/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VerticaReplicator object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.4/pkg/reconcile
func (r *VerticaReplicatorReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("vrep", req.NamespacedName)
	log.Info("starting reconcile of VerticaReplicator")

	vrep := &vapi.VerticaReplicator{}
	err := r.Get(ctx, req.NamespacedName, vrep)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaReplicator resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaReplicator")
		return ctrl.Result{}, err
	}

	if meta.IsPauseAnnotationSet(vrep.Annotations) {
		log.Info(fmt.Sprintf("The pause annotation %s is set. Suspending the iteration", meta.PauseOperatorAnnotation),
			"result", ctrl.Result{}, "err", nil)
		return ctrl.Result{}, nil
	}

	// Iterate over each actor
	actors := r.constructActors(vrep, log)
	var res ctrl.Result
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			log.Info("aborting reconcile of VerticaReplicator", "result", res, "err", err)
			return res, err
		}
	}

	log.Info("ending reconcile of VerticaReplicator", "result", res, "err", err)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaReplicatorReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.VerticaReplicator{}).
		Complete(r)
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *VerticaReplicatorReconciler) constructActors(vrep *vapi.VerticaReplicator,
	log logr.Logger) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a vrep.
	actors := []controllers.ReconcileActor{
		// Verify some checks before starting a replication
		MakeVdbVerifyReconciler(r, vrep, log),
		// Start a replication and update status accordingly upon its completion
		MakeReplicationReconciler(r, vrep, log),
	}
	return actors
}

// Event a wrapper for Event() that also writes a log entry
func (r *VerticaReplicatorReconciler) Event(vrep runtime.Object, eventtype, reason, message string) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Event(vrep, eventtype, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *VerticaReplicatorReconciler) Eventf(vrep runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(vrep, eventtype, reason, messageFmt, args...)
}

// GetClient gives access to the Kubernetes client
func (r *VerticaReplicatorReconciler) GetClient() client.Client {
	return r.Client
}

// GetEventRecorder gives access to the event recorder
func (r *VerticaReplicatorReconciler) GetEventRecorder() record.EventRecorder {
	return r.EVRec
}

// GetConfig gives access to *rest.Config
func (r *VerticaReplicatorReconciler) GetConfig() *rest.Config {
	return r.Cfg
}
