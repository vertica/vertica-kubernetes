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

package vrqb

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
	"github.com/vertica/vertica-kubernetes/pkg/meta"
)

// VerticaRestorePointsQueryReconciler reconciles a VerticaRestorePointsQuery object
type VerticaRestorePointsQueryReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	EVRec  record.EventRecorder
}

//+kubebuilder:rbac:groups=vertica.com,resources=verticarestorepointsqueries,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=vertica.com,resources=verticarestorepointsqueries/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=vertica.com,resources=verticarestorepointsqueries/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the VerticaRestorePointsQuery object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.14.1/pkg/reconcile
func (r *VerticaRestorePointsQueryReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := r.Log.WithValues("vrqb", req.NamespacedName)
	log.Info("starting reconcile of vertica restore point query")

	vrqb := &vapi.VerticaRestorePointsQuery{}
	err := r.Get(ctx, req.NamespacedName, vrqb)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, cound have been deleted after reconcile request.
			log.Info("VerticaRestorePointsQuery resource not found.  Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "failed to get VerticaRestorePointsQuery")
		return ctrl.Result{}, err
	}

	if meta.IsPauseAnnotationSet(vrqb.Annotations) {
		log.Info(fmt.Sprintf("The pause annotation %s is set. Suspending the iteration", meta.PauseOperatorAnnotation),
			"result", ctrl.Result{}, "err", nil)
		return ctrl.Result{}, nil
	}

	// Iterate over each actor
	actors := r.constructActors(vrqb, log)
	var res ctrl.Result
	for _, act := range actors {
		log.Info("starting actor", "name", fmt.Sprintf("%T", act))
		res, err = act.Reconcile(ctx, &req)
		// Error or a request to requeue will stop the reconciliation.
		if verrors.IsReconcileAborted(res, err) {
			log.Info("aborting reconcile of EventTrigger", "result", res, "err", err)
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *VerticaRestorePointsQueryReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&vapi.VerticaRestorePointsQuery{}).
		Complete(r)
}

// constructActors will a list of actors that should be run for the reconcile.
// Order matters in that some actors depend on the successeful execution of
// earlier ones.
func (r *VerticaRestorePointsQueryReconciler) constructActors(_ *vapi.VerticaRestorePointsQuery,
	_ logr.Logger) []controllers.ReconcileActor {
	// The actors that will be applied, in sequence, to reconcile an et.
	// Temporarily, we set nil value for constructActors
	return nil
}
