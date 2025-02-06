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

	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// HorizontalPodAutoscalerReconciler is a reconciler to handle horizontal pod autoscaler
// creation and update.
type HorizontalPodAutoscalerReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Log  logr.Logger
}

func MakeHorizontalPodAutoscalerReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler,
	log logr.Logger) controllers.ReconcileActor {
	return &HorizontalPodAutoscalerReconciler{VRec: v, Vas: vas, Log: log.WithName("HorizontalPodAutoscalerReconciler")}
}

// Reconcile will handle creating the hpa if it does not exist or updating
// the hpa if its spec is different from the CR's.
func (h *HorizontalPodAutoscalerReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !h.Vas.IsCustomMetricsEnabled() {
		return ctrl.Result{}, nil
	}
	nm := names.GenHPAName(h.Vas)
	curHpa := &autoscalingv2.HorizontalPodAutoscaler{}
	expHpa := builder.BuildHorizontalPodAutoscaler(nm, h.Vas)
	err := h.VRec.Client.Get(ctx, nm, curHpa)
	if err != nil && kerrors.IsNotFound(err) {
		h.Log.Info("Creating horizontalpodautoscaler", "Name", nm.Name)
		return ctrl.Result{}, createHpa(ctx, h.VRec, expHpa, h.Vas)
	}
	if h.Vas.HasScaleDownThreshold() {
		// We keep the current value because it will be changed elsewhere.
		*expHpa.Spec.MinReplicas = *curHpa.Spec.MinReplicas
	}
	return ctrl.Result{}, h.updateHPA(ctx, curHpa, expHpa)
}

func (h *HorizontalPodAutoscalerReconciler) updateHPA(ctx context.Context, curHpa, expHpa *autoscalingv2.HorizontalPodAutoscaler) error {
	// Create a patch object
	patch := client.MergeFrom(curHpa.DeepCopy())
	origHPA := curHpa.DeepCopy()

	// Copy the Spec, Labels, and Annotations
	expHpa.Spec.DeepCopyInto(&curHpa.Spec)
	curHpa.SetLabels(expHpa.GetLabels())
	curHpa.SetAnnotations(expHpa.GetAnnotations())

	// Patch the HPA
	if err := h.VRec.Client.Patch(ctx, curHpa, patch); err != nil {
		return err
	}

	// Check if the spec was modified
	if !reflect.DeepEqual(curHpa, origHPA) {
		h.Log.Info("Patched HPA",
			"Name", curHpa.Name,
			"MinReplicas", *curHpa.Spec.MinReplicas,
			"MaxReplicas", curHpa.Spec.MaxReplicas)
	}

	return nil
}
