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
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SubclusterScaleReconciler will scale a VerticaDB by adding or removing subclusters.
type SubclusterScaleReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Vdb  *vapi.VerticaDB
}

func MakeSubclusterScaleReconciler(r *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) controllers.ReconcileActor {
	return &SubclusterScaleReconciler{VRec: r, Vas: vas, Vdb: &vapi.VerticaDB{}}
}

// Reconcile will grow/shrink the VerticaDB passed on the target pod count.
func (s *SubclusterScaleReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if s.Vas.Spec.ScalingGranularity != vapi.SubclusterScalingGranularity {
		return ctrl.Result{}, nil
	}

	return s.scaleSubcluster(ctx, req)
}

// scaleSubcluster will decide to add or remove whole subclusters to reach the
// target size
func (s *SubclusterScaleReconciler) scaleSubcluster(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result
	scalingDone := false
	// Update the VerticaDB with a retry mechanism for any conflict updates
	// (i.e. if someone updated the vdb since we last fetched it)
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r, e := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb); verrors.IsReconcileAborted(r, e) {
			res = r
			return e
		}

		_, totSize := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.ServiceName)
		delta := s.Vas.Spec.TargetSize - totSize
		switch {
		case delta < 0:
			if changed := s.considerRemovingSubclusters(delta * -1); !changed {
				return nil
			}
		case delta > 0:
			if changed := s.considerAddingSubclusters(delta); !changed {
				return nil
			}
		default:
			return nil // No change
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

// considerRemovingSubclusters will shrink the Vdb by removing subclusters --
// picking the last one first.  Changes are made in-place in s.Vdb
func (s *SubclusterScaleReconciler) considerRemovingSubclusters(podsToRemove int32) bool {
	origNumSubclusters := len(s.Vdb.Spec.Subclusters)
	for j := len(s.Vdb.Spec.Subclusters) - 1; j >= 0; j-- {
		sc := &s.Vdb.Spec.Subclusters[j]
		if sc.GetServiceName() == s.Vas.Spec.ServiceName {
			if podsToRemove > 0 && sc.Size <= podsToRemove {
				podsToRemove -= sc.Size
				s.VRec.Log.Info("Removing subcluster in VerticaDB", "VerticaDB", s.Vdb.Name, "Subcluster", sc.Name)
				s.Vdb.Spec.Subclusters = append(s.Vdb.Spec.Subclusters[:j], s.Vdb.Spec.Subclusters[j+1:]...)
				continue
			} else {
				return origNumSubclusters != len(s.Vdb.Spec.Subclusters)
			}
		}
	}
	return origNumSubclusters != len(s.Vdb.Spec.Subclusters)
}

// considerAddingSubclusters will grow the Vdb by adding new subclusters.
// Changes are made in-place in s.Vdb
func (s *SubclusterScaleReconciler) considerAddingSubclusters(newPodsNeeded int32) bool {
	origNumSubclusters := len(s.Vdb.Spec.Subclusters)
	scMap := s.Vdb.GenSubclusterMap()
	newScSize, ok := s.calcNextSubclusterSize(scMap)
	if !ok {
		return false
	}
	for newPodsNeeded >= newScSize {
		newSc, _ := s.calcNextSubcluster(scMap)
		s.Vdb.Spec.Subclusters = append(s.Vdb.Spec.Subclusters, *newSc)
		scMap[newSc.Name] = &s.Vdb.Spec.Subclusters[len(s.Vdb.Spec.Subclusters)-1]
		newPodsNeeded -= newSc.Size
		s.VRec.Log.Info("Adding subcluster to VerticaDB", "VerticaDB", s.Vdb.Name, "Subcluster", newSc.Name, "Size", newSc.Size)
	}
	return origNumSubclusters != len(s.Vdb.Spec.Subclusters)
}

// genNextSubclusterName will come up with a unique name to give a new subcluster
func (s *SubclusterScaleReconciler) genNextSubclusterName(scMap map[string]*vapi.Subcluster) string {
	baseName := s.Vas.Spec.Template.Name
	if baseName == "" {
		baseName = s.Vas.Name
	}
	i := 0
	for {
		name := fmt.Sprintf("%s-%d", baseName, i)
		_, ok := scMap[name]
		if !ok {
			return name
		}
		i++
	}
}

// calcNextSubclusterSize returns the size of the next subcluster
func (s *SubclusterScaleReconciler) calcNextSubclusterSize(scMap map[string]*vapi.Subcluster) (int32, bool) {
	newSc, ok := s.calcNextSubcluster(scMap)
	if !ok {
		return 0, false
	}
	return newSc.Size, true
}

// calcNextSubcluster build the next subcluster that we want to add to the vdb.
// Returns false for second parameter if unable to construct one.  An event will
// be logged if this happens.
func (s *SubclusterScaleReconciler) calcNextSubcluster(scMap map[string]*vapi.Subcluster) (*vapi.Subcluster, bool) {
	// If the template is set, we will use that.  Otherwise, we try to use an
	// existing subcluster (last one added) as a base.
	if s.Vas.CanUseTemplate() {
		sc := s.Vas.Spec.Template.DeepCopy()
		sc.Name = s.genNextSubclusterName(scMap)
		return sc, true
	}
	scs, _ := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.ServiceName)
	if len(scs) == 0 {
		msg := "Could not determine size of the next subcluster.  Template in VerticaAutoscaler "
		msg += "is empty and no existing subcluster can be used as a base"
		s.VRec.Log.Info(msg)
		s.VRec.EVRec.Event(s.Vas, corev1.EventTypeWarning, events.NoSubclusterTemplate, msg)
		return nil, false
	}
	newSc := scs[len(scs)-1].DeepCopy()
	newSc.ServiceName = s.Vas.Spec.ServiceName
	newSc.Name = s.genNextSubclusterName(scMap)
	return newSc, true
}
