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

package vas

import (
	"context"
	"fmt"

	"reflect"

	"github.com/go-logr/logr"
	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ObjReconciler is a reconciler to handle reconciliation of VerticaAutoscaler-owned
// objects.
type ObjReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Log  logr.Logger
}

func MakeObjReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler,
	log logr.Logger) controllers.ReconcileActor {
	return &ObjReconciler{VRec: v, Vas: vas, Log: log.WithName("ObjReconciler")}
}

// Reconcile will handle creating the hpa/scaledObject if it does not exist or updating
// the hpa/scaledObject if its spec is different from the CR's.
func (o *ObjReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !o.Vas.IsCustomMetricsEnabled() {
		return ctrl.Result{}, nil
	}
	if o.Vas.IsHpaEnabled() {
		return ctrl.Result{}, o.reconcileHpa(ctx)
	}
	return ctrl.Result{}, o.reconcileScaledObject(ctx)
}

// reconcileHpa creates a new hpa or updates an existing one.
func (o *ObjReconciler) reconcileHpa(ctx context.Context) error {
	nm := names.GenHPAName(o.Vas)
	curHpa := &autoscalingv2.HorizontalPodAutoscaler{}
	expHpa := builder.BuildHorizontalPodAutoscaler(nm, o.Vas)
	err := o.VRec.Client.Get(ctx, nm, curHpa)
	if err != nil && kerrors.IsNotFound(err) {
		o.Log.Info("Creating horizontalpodautoscaler", "Name", nm.Name)
		return createObject(ctx, expHpa, o.VRec.Client, o.Vas)
	}
	if o.Vas.HasScaleDownThreshold() {
		// We keep the current value because it will be changed elsewhere.
		*expHpa.Spec.MinReplicas = *curHpa.Spec.MinReplicas
	}
	return o.updateWorkload(ctx, curHpa, expHpa)
}

// reconcileScaledObject creates a scaledObject or updates an existing one.
func (o *ObjReconciler) reconcileScaledObject(ctx context.Context) error {
	nm := names.GenScaledObjectName(o.Vas)
	curSO := &kedav1alpha1.ScaledObject{}
	expSO := builder.BuildScaledObject(nm, o.Vas)
	err := o.VRec.Client.Get(ctx, nm, curSO)
	if err != nil && kerrors.IsNotFound(err) {
		o.Log.Info("Creating scaledobject", "Name", nm.Name)
		return createObject(ctx, expSO, o.VRec.Client, o.Vas)
	}
	return o.updateWorkload(ctx, curSO, expSO)
}

func (o *ObjReconciler) updateWorkload(ctx context.Context, curWorkload, expWorkload client.Object) error {
	// Create a patch object
	patch := client.MergeFrom(curWorkload.DeepCopyObject().(client.Object))
	origWorkload := curWorkload.DeepCopyObject().(client.Object)

	// Copy Spec, Labels, and Annotations
	switch cw := curWorkload.(type) {
	case *autoscalingv2.HorizontalPodAutoscaler:
		expHpa := expWorkload.(*autoscalingv2.HorizontalPodAutoscaler)
		expHpa.Spec.DeepCopyInto(&cw.Spec)
	case *kedav1alpha1.ScaledObject:
		expSO := expWorkload.(*kedav1alpha1.ScaledObject)
		expSO.Spec.DeepCopyInto(&cw.Spec)
	default:
		return fmt.Errorf("unsupported workload type: %T", curWorkload)
	}
	curWorkload.SetLabels(expWorkload.GetLabels())
	curWorkload.SetAnnotations(expWorkload.GetAnnotations())

	// Patch the workload
	if err := o.VRec.Client.Patch(ctx, curWorkload, patch); err != nil {
		return err
	}

	// Check if the spec was modified
	if !reflect.DeepEqual(curWorkload, origWorkload) {
		if hpa, ok := curWorkload.(*autoscalingv2.HorizontalPodAutoscaler); ok {
			o.Log.Info("Patched HPA",
				"Name", hpa.Name,
				"MinReplicas", *hpa.Spec.MinReplicas,
				"MaxReplicas", hpa.Spec.MaxReplicas)
		} else {
			so := curWorkload.(*kedav1alpha1.ScaledObject)
			o.Log.Info("Patched ScaledObject",
				"Name", so.Name,
				"MinReplicas", *so.Spec.MinReplicaCount,
				"MaxReplicas", *so.Spec.MaxReplicaCount)
		}
	}
	return nil
}
