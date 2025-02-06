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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VerifyHPAReconciler is a reconciler to check if the hpa is ready for scaling.
type VerifyHPAReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Log  logr.Logger
}

func MakeVerifyHPAReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler,
	log logr.Logger) controllers.ReconcileActor {
	return &VerifyHPAReconciler{VRec: v, Vas: vas, Log: log.WithName("VerifyHPAReconciler")}
}

// Reconcile will check the hpa status and requeue if the hpa is not ready.
func (v *VerifyHPAReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !v.Vas.IsCustomMetricsEnabled() {
		return ctrl.Result{}, nil
	}
	nm := names.GenHPAName(v.Vas)
	curHpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := v.VRec.Client.Get(ctx, nm, curHpa)
	if err != nil {
		v.Log.Info("Cannot get the hpa. Requeueing to retry.", "hpa", nm.Name)
		return ctrl.Result{Requeue: true}, err
	}
	conds := curHpa.Status.Conditions
	cond := vapi.VerticaAutoscalerCondition{
		Type:   vapi.ScalingActive,
		Status: corev1.ConditionFalse,
	}
	scalingActive := isStatusConditionPresentAndEqual(conds, autoscalingv2.ScalingActive, corev1.ConditionTrue) &&
		len(curHpa.Status.CurrentMetrics) == len(v.Vas.Spec.CustomAutoscaler.Metrics)
	if scalingActive {
		cond.Status = corev1.ConditionTrue
	} else {
		v.Log.Info("hpa is not ready for autoscaling. Requeueing", "hpa", nm.Name)
	}
	return ctrl.Result{Requeue: !scalingActive}, vasstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, req, cond)
}

func isStatusConditionPresentAndEqual(conditions []autoscalingv2.HorizontalPodAutoscalerCondition,
	conditionType autoscalingv2.HorizontalPodAutoscalerConditionType, status corev1.ConditionStatus) bool {
	for _, condition := range conditions {
		if condition.Type == conditionType {
			return condition.Status == status
		}
	}
	return false
}
