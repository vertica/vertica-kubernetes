/*
Copyright [2021-2023] Open Text.
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

package vscr

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
)

// VerticaScrutinizeReconciler reconciles a VerticaScrutinize object
type VerticaScrutinizeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	EVRec  record.EventRecorder
}

const (
	vdbNameField = ".spec.verticaDBName"
)

//+kubebuilder:rbac:groups=vertica.com,resources=verticascrutinizers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,resources=verticascrutinizers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,resources=verticascrutinizers/finalizers,verbs=update
//+kubebuilder:rbac:groups=vertica.com,resources=verticadbs,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VerticaScrutinize object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *VerticaScrutinizeReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("vscr", req.NamespacedName)
	log.Info("starting reconcile of vertica scrutinize")

	vscr := &v1beta1.VerticaScrutinize{}
	err := r.Get(ctx, req.NamespacedName, vscr)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaScrutinize resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaScrutinize")
		return ctrl.Result{}, err
	}

	if meta.IsPauseAnnotationSet(vscr.Annotations) {
		log.Info(fmt.Sprintf("The pause annotation %s is set. Suspending the iteration", meta.PauseOperatorAnnotation),
			"result", ctrl.Result{}, "err", nil)
		return ctrl.Result{}, nil
	}

	if ok, reason := r.abortReconcile(vscr); ok {
		r.logScrutinizeNotReadyMsg(log, vscr.Spec.VerticaDBName, reason)
		return ctrl.Result{}, nil
	}

	// Iterate over each actor
	actors := r.constructActors(vscr, log)
	var res ctrl.Result
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			log.Info("aborting reconcile of VerticaScrutinize", "result", res, "err", err)
			return res, err
		}
	}

	log.Info("ending reconcile of VerticaScrutinize", "result", res, "err", err)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaScrutinizeReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.VerticaScrutinize{}).
		Owns(&corev1.Pod{}).
		Watches(
			&source.Kind{Type: &v1.VerticaDB{}},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForVerticaDB),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// findObjectsForVerticaDB will generate requests to reconcile VerticaScrutiners
// based on watched VerticaDB.
func (r *VerticaScrutinizeReconciler) findObjectsForVerticaDB(vdb client.Object) []reconcile.Request {
	scrutinizers := &v1beta1.VerticaScrutinizeList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(vdbNameField, vdb.GetName()),
		Namespace:     vdb.GetNamespace(),
	}
	err := r.List(context.Background(), scrutinizers, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(scrutinizers.Items))
	for i := range scrutinizers.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      scrutinizers.Items[i].GetName(),
				Namespace: scrutinizers.Items[i].GetNamespace(),
			},
		}
	}
	return requests
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *VerticaScrutinizeReconciler) constructActors(vscr *v1beta1.VerticaScrutinize,
	log logr.Logger) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile a vscr.
	return []controllers.ReconcileActor{
		MakeVDBVerifyReconciler(r, vscr, log),
		MakeScrutinizePodReconciler(r, vscr, log),
	}
}

// Event a wrapper for Event() that also writes a log entry
func (r *VerticaScrutinizeReconciler) Event(vdb runtime.Object, eventtype, reason, message string) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Event(vdb, eventtype, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (r *VerticaScrutinizeReconciler) Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   r.Log,
		EVRec: r.EVRec,
	}
	evWriter.Eventf(vdb, eventtype, reason, messageFmt, args...)
}

// GetClient gives access to the Kubernetes client
func (r *VerticaScrutinizeReconciler) GetClient() client.Client {
	return r.Client
}

// abortReconcile returns true if it is not the first reconciliation iteration and VerticaDB is not
// configured for vclusterops scrutinize
func (r *VerticaScrutinizeReconciler) abortReconcile(vscr *v1beta1.VerticaScrutinize) (ok bool, reason string) {
	cond := vscr.FindStatusCondition(v1beta1.ScrutinizeReady)
	if cond == nil {
		// this is likely the first reconciliation iteration
		return false, ""
	}
	ok = cond.Status == metav1.ConditionFalse
	reason = cond.Reason
	return
}

// logScrutinizeNotReadyMsg logs a non-error message when ScrutinizeReady is false
// after one reconciliation iteration
func (r *VerticaScrutinizeReconciler) logScrutinizeNotReadyMsg(log logr.Logger, vdbName, reason string) {
	var msg string
	switch reason {
	case events.VerticaDBNotFound:
		msg = fmt.Sprintf("VerticaDB %s not found. Must exist before the VerticaScrutinize resource is created.",
			vdbName)
	case events.VclusterOpsDisabled:
		msg = fmt.Sprintf("VerticaDB %s has vclusterOps disabled.", vdbName)
	case events.VerticaVersionNotFound:
		msg = fmt.Sprintf("The server version could not be found in the VerticaDB %s", vdbName)
	default:
		msg = "The server version does not have scrutinize support through vclusterOps"
	}
	log.Info(msg)
}
