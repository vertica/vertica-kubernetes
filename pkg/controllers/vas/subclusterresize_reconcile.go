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

package vas

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SubclusterResizeReconciler will grow/shrink existing subclusters based on the
// target pod count in the CR.
type SubclusterResizeReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Vdb  *vapi.VerticaDB
}

func MakeSubclusterResizeReconciler(r *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) controllers.ReconcileActor {
	return &SubclusterResizeReconciler{VRec: r, Vas: vas, Vdb: &vapi.VerticaDB{}}
}

// Reconcile will grow/shrink an existing subcluste based on the target pod count
func (s *SubclusterResizeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if s.Vas.Spec.ScalingGranularity != vapi.PodScalingGranularity {
		return ctrl.Result{}, nil
	}

	return s.resizeSubcluster(ctx, req)
}

// resizeSubcluster will change the size of a subcluster given the target pod count
func (s *SubclusterResizeReconciler) resizeSubcluster(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result
	scalingDone := false
	// Update the VerticaDB with a retry mechanism for any conflict updates
	// (i.e. if someone updated the vdb since we last fetched it)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r, e := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb); verrors.IsReconcileAborted(r, e) {
			res = r
			return e
		}

		subclusters, totSize := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.ServiceName)
		if len(subclusters) == 0 {
			s.VRec.EVRec.Eventf(s.Vas, corev1.EventTypeWarning, events.SubclusterServiceNameNotFound,
				"Could not find any subclusters with service name '%s'", s.Vas.Spec.ServiceName)
			res.Requeue = true
			return nil
		}

		delta := s.Vas.Spec.TargetSize - totSize
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

		err := s.VRec.Client.Update(ctx, s.Vdb)
		if err == nil {
			scalingDone = true
		}
		return err
	})

	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	if scalingDone {
		_, totSize := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.ServiceName)
		err = vasstatus.ReportScalingOperation(ctx, s.VRec.Client, s.VRec.Log, req, totSize)
	}

	return res, err
}
