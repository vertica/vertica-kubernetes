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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DepObjCheckReconciler will ensure all dependent objects exist. If any are
// missing, then the reconcile iteration will requeue.
type DepObjCheckReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

// MakeDepObjCheckReconciler will build a DepObjCheckReconciler object
func MakeDepObjCheckReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &DepObjCheckReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("DepObjCheckReconciler"),
	}
}

// Reconcile will check that dependent objects exists that the operator creates.
// It leaves it up to other reconciler to create them. This is just meant as a
// final verification before concluding the reconcile iteration. Omitted from
// this reconciler are objects that user creates, such as secrets.
func (d *DepObjCheckReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	res, err := d.reconcileCRScopedObjs(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	for i := range d.Vdb.Spec.Subclusters {
		res, err = d.reconcileSubclusterScopedObjs(ctx, &d.Vdb.Spec.Subclusters[i])
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileCRScopedObjs will check for dependent objects that share the same
// scope as the VerticaDB.
func (d *DepObjCheckReconciler) reconcileCRScopedObjs(ctx context.Context) (ctrl.Result, error) {
	checkers := []func(context.Context) (ctrl.Result, error){
		d.checkHlSvc,
		d.checkSvcAccount,
	}
	for i := range checkers {
		if res, err := checkers[i](ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileSubclusterScopedObjs will check for dependent objects that share the
// same scope as a subcluster
func (d *DepObjCheckReconciler) reconcileSubclusterScopedObjs(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	checkers := []func(context.Context, *vapi.Subcluster) (ctrl.Result, error){
		d.checkExtSvc,
		d.checkSts,
		d.checkPods,
	}
	for i := range checkers {
		if res, err := checkers[i](ctx, sc); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// checkObj helper function that will check a specific to object if it exists.
func (d *DepObjCheckReconciler) checkObj(ctx context.Context, objType string, nm types.NamespacedName, obj client.Object) (
	ctrl.Result, error) {
	err := d.VRec.Client.Get(ctx, nm, obj)
	if err != nil {
		if kerrors.IsNotFound(err) {
			d.Log.Info("Requeue because dependent object doesn't exist", "objType", objType, "name", nm)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// All of the check* functions below are checking for specific objects to exist.

func (d *DepObjCheckReconciler) checkHlSvc(ctx context.Context) (ctrl.Result, error) {
	return d.checkObj(ctx, "Headless service", names.GenHlSvcName(d.Vdb), &corev1.Service{})
}

func (d *DepObjCheckReconciler) checkSvcAccount(ctx context.Context) (ctrl.Result, error) {
	if d.Vdb.Spec.ServiceAccountName != "" {
		return d.checkObj(ctx, "ServiceAccount", names.GenNamespacedName(d.Vdb, d.Vdb.Spec.ServiceAccountName), &corev1.ServiceAccount{})
	}
	return ctrl.Result{}, nil
}

func (d *DepObjCheckReconciler) checkExtSvc(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	return d.checkObj(ctx, "Service", names.GenExtSvcName(d.Vdb, sc), &corev1.Service{})
}

func (d *DepObjCheckReconciler) checkSts(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	return d.checkObj(ctx, "Statefulset", names.GenStsName(d.Vdb, sc), &appsv1.StatefulSet{})
}

func (d *DepObjCheckReconciler) checkPods(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	scStatus, found := d.Vdb.GenSubclusterStatusMap()[sc.Name]
	// Ignore subclusters that are shut down
	if sc.Shutdown || (found && scStatus.Shutdown) {
		return ctrl.Result{}, nil
	}
	for i := int32(0); i < sc.Size; i++ {
		if res, err := d.checkObj(ctx, "Pod", names.GenPodName(d.Vdb, sc, i), &corev1.Pod{}); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}
