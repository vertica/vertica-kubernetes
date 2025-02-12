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

	"github.com/go-logr/logr"
	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VerifyAutoscalerReconciler is a reconciler to check if the autoscaler is ready for scaling.
type VerifyAutoscalerReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *v1beta1.VerticaAutoscaler
	Log  logr.Logger
}

func MakeVerifyAutoscalerReconciler(v *VerticaAutoscalerReconciler, vas *v1beta1.VerticaAutoscaler,
	log logr.Logger) controllers.ReconcileActor {
	return &VerifyAutoscalerReconciler{VRec: v, Vas: vas, Log: log.WithName("VerifyAutoscalerReconciler")}
}

// Reconcile will check the hpa status and requeue if the hpa is not ready.
func (v *VerifyAutoscalerReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !v.Vas.IsCustomMetricsEnabled() {
		return ctrl.Result{}, nil
	}

	cond, err := v.getCondition(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	scalingActive := cond.Status == corev1.ConditionTrue
	if !scalingActive {
		v.Log.Info("autoscaler is not ready. Requeueing")
	}

	return ctrl.Result{Requeue: !scalingActive}, vasstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, req, *cond)
}

func isStatusConditionPresentAndEqualHpa(conditions []autoscalingv2.HorizontalPodAutoscalerCondition,
	conditionType autoscalingv2.HorizontalPodAutoscalerConditionType, status corev1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}

func isStatusConditionPresentAndEqualKeda(conditions kedav1alpha1.Conditions,
	conditionType kedav1alpha1.ConditionType, status metav1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}

// getCondition returns the condition status that determines if the autoscaler is ready.
func (v *VerifyAutoscalerReconciler) getCondition(ctx context.Context) (*v1beta1.VerticaAutoscalerCondition, error) {
	cond := &v1beta1.VerticaAutoscalerCondition{
		Type:   v1beta1.ScalingActive,
		Status: corev1.ConditionFalse,
	}
	if v.Vas.IsHpaEnabled() {
		nm := names.GenHPAName(v.Vas)
		curHpa := &autoscalingv2.HorizontalPodAutoscaler{}
		err := v.VRec.Client.Get(ctx, nm, curHpa)
		if err != nil {
			v.Log.Info("Cannot get the hpa. Requeueing to retry.", "hpa", nm.Name)
			return nil, err
		}
		scalingActive := isStatusConditionPresentAndEqualHpa(curHpa.Status.Conditions, autoscalingv2.ScalingActive, corev1.ConditionTrue) &&
			len(curHpa.Status.CurrentMetrics) == len(v.Vas.Spec.CustomAutoscaler.Hpa.Metrics)
		if scalingActive {
			cond.Status = corev1.ConditionTrue
		}
	} else {
		nm := names.GenScaledObjectName(v.Vas)
		curSO := &kedav1alpha1.ScaledObject{}
		err := v.VRec.Client.Get(ctx, nm, curSO)
		if err != nil {
			v.Log.Info("Cannot get the scaledObject. Requeueing to retry.", "scaledObject", nm.Name)
			return nil, err
		}
		scalingActive := isStatusConditionPresentAndEqualKeda(curSO.Status.Conditions, kedav1alpha1.ConditionReady, metav1.ConditionTrue)
		if scalingActive {
			cond.Status = corev1.ConditionTrue
		}
	}
	return cond, nil
}
