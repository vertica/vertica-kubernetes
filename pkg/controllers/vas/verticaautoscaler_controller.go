/*
Copyright [2021-2022] Micro Focus or one of its affiliates.

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

package vas

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
)

// VerticaAutoscalerReconciler reconciles a VerticaAutoscaler object
type VerticaAutoscalerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	EVRec  record.EventRecorder
}

//nolint:lll
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticaautoscalers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticaautoscalers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticaautoscalers/finalizers,verbs=update
//+kubebuilder:rbac:groups=vertica.com,namespace=WATCH_NAMESPACE,resources=verticadbs,verbs=get;list;create;update;patch;delete

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *VerticaAutoscalerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("verticaautoscaler", req.NamespacedName)
	log.Info("starting reconcile of VerticaAutoscaler")

	var res ctrl.Result
	vas := &vapi.VerticaAutoscaler{}
	err := r.Get(ctx, req.NamespacedName, vas)
	if err != nil {
		if errors.IsNotFound(err) {
			log.Info("VerticaAutoscaler resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaAutoscaler")
		return ctrl.Result{}, err
	}

	// The actors that will be applied, in sequence, to reconcile a vas.
	actors := []controllers.ReconcileActor{
		// Sanity check to make sure the VerticaDB referenced in vas actually exists.
		MakeVDBVerifyReconciler(r, vas),
		// Initialize targetSize in new VerticaAutoscaler objects
		MakeTargetSizeInitializerReconciler(r, vas),
		// Update the currentSize in the status
		MakeRefreshCurrentSizeReconciler(r, vas),
		// Update the selector in the status
		MakeRefreshSelectorReconciler(r, vas),
		// If scaling granularity is Pod, this will resize existing subclusters
		// depending on the targetSize.
		MakeSubclusterResizeReconciler(r, vas),
		// If scaling granularity is Subcluster, this will create or delete
		// entire subcluster to match the targetSize.
		MakeSubclusterScaleReconciler(r, vas),
	}

	// Iterate over each actor
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			log.Info("aborting reconcile of VerticaAutoscaler", "result", res, "err", err)
			return res, err
		}
	}

	log.Info("ending reconcile of VerticaAutoscaler", "result", res, "err", err)
	return res, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.VerticaAutoscaler{}).
		// Not a strict ownership, but this is used so that the operator will
		// reconcile the VerticaAutoscaler for any change in the VerticaDB.
		// This ensures the status fields are kept up to date.
		Owns(&vapi.VerticaDB{}).
		Complete(r)
}
