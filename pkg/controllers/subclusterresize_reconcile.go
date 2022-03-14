/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StatusReconciler will update the status field of the vdb.
type SubclusterResizeReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Vdb  *vapi.VerticaDB
}

// MakeStatusReconciler will build a StatusReconciler object
func MakeSubclusterResizeReconciler(r *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) ReconcileActor {
	return &SubclusterResizeReconciler{VRec: r, Vas: vas, Vdb: &vapi.VerticaDB{}}
}

// Reconcile will update the status of the Vdb based on the pod facts
func (s *SubclusterResizeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if s.Vas.Spec.ScalingGranularity != vapi.PodScalingGranularity {
		return ctrl.Result{}, nil
	}

	if res, err := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	if s.Vas.Spec.TargetSize == 0 {
		s.VRec.Log.Info("Target not set yet in VerticaAutoscaler")
		return ctrl.Result{}, nil
	}

	return s.resizeSubcluster(ctx)
}

// resizeSubcluster will change the size of a subcluster given the target pod count
func (s *SubclusterResizeReconciler) resizeSubcluster(ctx context.Context) (ctrl.Result, error) {
	var res ctrl.Result
	// Update the VerticaAutoscaler with a retry mechanism for any conflict updates
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r, e := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb); verrors.IsReconcileAborted(r, e) {
			res = r
			return e
		}

		subclusters := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.SubclusterServiceName)
		if len(subclusters) == 0 {
			s.VRec.EVRec.Eventf(s.Vas, corev1.EventTypeWarning, events.VerticaDBNotFound,
				"Could not find any subclusters with service name '%s'", s.Vas.Spec.SubclusterServiceName)
			res.Requeue = true
			return nil
		}

		delta := s.findResizeDelta(subclusters)
		if delta == 0 {
			return nil
		}

		for i := len(subclusters) - 1; i >= 0; i-- {
			targetSc := subclusters[i]
			if delta > 0 { // Growing subclusters
				targetSc.Size += delta
				delta = 0
			} else { // Shrinking subclusters
				if -1*delta > targetSc.Size {
					delta += targetSc.Size
					targetSc.Size = 0
				} else {
					targetSc.Size += delta
					delta = 0
				}
			}
			if delta == 0 {
				break
			}
		}

		return s.VRec.Client.Update(ctx, s.Vdb)
	})

	return res, err
}

// findResizeDelta determines the change that must occur to reach the target
// size.  If the number is positive, then we are going to increase the size of a
// subcluster.  If the number is negative, we need to decrease the size.
func (s *SubclusterResizeReconciler) findResizeDelta(subclusters []*vapi.Subcluster) int32 {
	var curSize int32
	for i := range subclusters {
		curSize += subclusters[i].Size
	}
	return s.Vas.Spec.TargetSize - curSize
}
