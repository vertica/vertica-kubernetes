/*
Copyright [2021-2023] Micro Focus or one of its affiliates.

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

package et

import (
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
)

// EventTriggerReconciler reconciles a EventTrigger object
type EventTriggerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	vdbNameField = ".spec.references.object.name"
)

//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=eventtriggers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=eventtriggers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=eventtriggers/finalizers,verbs=update
//+kubebuilder:rbac:groups="batch",namespace=WATCH_NAMESPACE,resources=jobs,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *EventTriggerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	lg := log.FromContext(ctx).WithValues("et", req.NamespacedName)
	lg.Info("starting reconcile of EventTrigger")

	et := &vapi.EventTrigger{}
	err := r.Get(ctx, req.NamespacedName, et)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, cound have been deleted after reconcile request.
			lg.Info("EventTrigger resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		lg.Error(err, "failed to get EventTrigger")
		return ctrl.Result{}, err
	}

	// Iterate over each actor
	actors := r.constructActors(lg, et)
	var res ctrl.Result
	for _, act := range actors {
		lg.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			lg.Info("aborting reconcile of VerticaDB", "result", res, "err", err)
			return res, err
		}
	}

	lg.Info("ending reconcile of EventTrigger", "result", res, "err", err)
	return res, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *EventTriggerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.setupFieldIndexer(mgr.GetFieldIndexer()); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.EventTrigger{}).
		Owns(&batchv1.Job{}).
		Watches(
			&source.Kind{Type: &vapi.VerticaDB{}},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForVerticaDB),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

// setupFieldIndexer will setup an index over object names. This allows us to
// lookup VerticaDB by name in the reference object.
func (r *EventTriggerReconciler) setupFieldIndexer(indx client.FieldIndexer) error {
	return indx.IndexField(context.Background(), &vapi.EventTrigger{}, vdbNameField, func(rawObj client.Object) []string {
		var res []string
		for _, ref := range rawObj.(*vapi.EventTrigger).Spec.References {
			if ref.Object == nil {
				continue
			}
			if ref.Object.Kind != vapi.VerticaDBKind {
				continue
			}
			res = append(res, ref.Object.Name)
		}
		return res
	})
}

// findObjectsForVerticaDB will generate requests to reconcile EventTriggers
// based on watched VerticaDB.
func (r *EventTriggerReconciler) findObjectsForVerticaDB(vdb client.Object) []reconcile.Request {
	attachedTriggers := &vapi.EventTriggerList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(vdbNameField, vdb.GetName()),
		Namespace:     vdb.GetNamespace(),
	}
	err := r.List(context.Background(), attachedTriggers, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(attachedTriggers.Items))
	for i := range attachedTriggers.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      attachedTriggers.Items[i].GetName(),
				Namespace: attachedTriggers.Items[i].GetNamespace(),
			},
		}
	}
	return requests
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *EventTriggerReconciler) constructActors(lg logr.Logger, et *vapi.EventTrigger) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile an et.
	return []controllers.ReconcileActor{}
}
