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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ScaledownReconciler is a reconciler handle scale down when a lower
// threshold is set.
type ScaledownReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Log  logr.Logger
}

func MakeScaledownReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler,
	log logr.Logger) controllers.ReconcileActor {
	return &ScaledownReconciler{VRec: v, Vas: vas, Log: log.WithName("ScaledownReconciler")}
}

// Reconcile will handle updating the hpa based on the metrics current value.
// Only metrics with a scale down threshold set are taken into account.
func (s *ScaledownReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !s.Vas.HasScaleDownThreshold() {
		return ctrl.Result{}, nil
	}
	nm := names.GenHPAName(s.Vas)
	curHpa := &autoscalingv2.HorizontalPodAutoscaler{}
	err := s.VRec.Client.Get(ctx, nm, curHpa)
	if err != nil {
		s.Log.Info("Cannot get the hpa. Requeueing to retry.", "Name", nm.Name)
		return ctrl.Result{Requeue: true}, err
	}
	mMap := s.Vas.GetMetricMap()
	var newMinReplicas int32
	for i := range curHpa.Status.CurrentMetrics {
		cm := &curHpa.Status.CurrentMetrics[i]
		mStatus := getCurrentMetricStatus(cm)
		if mStatus == nil {
			continue
		}
		md, found := mMap[mStatus.name]
		if !found {
			return ctrl.Result{}, fmt.Errorf("could not find metric %s in VAS spec", mStatus.name)
		}
		if md.ScaleDownThreshold == nil {
			// skip the metric because it does not have scale down threshold.
			continue
		}
		cmpResult := mStatus.cmp(md.ScaleDownThreshold)
		if cmpResult == errorCmpResult {
			return ctrl.Result{}, errors.New("hpa status not set correctly")
		}
		if cmpResult < 0 {
			s.Log.Info("Metric's value is lower than the scale-down threshold.", "metric", mStatus.name)
			newMinReplicas = *s.Vas.Spec.CustomAutoscaler.Hpa.MinReplicas
		} else {
			newMinReplicas = s.Vas.Status.CurrentSize
			break
		}
	}
	if *curHpa.Spec.MinReplicas != newMinReplicas {
		*curHpa.Spec.MinReplicas = newMinReplicas
		return ctrl.Result{}, s.VRec.Client.Update(ctx, curHpa)
	}
	return ctrl.Result{}, nil
}
