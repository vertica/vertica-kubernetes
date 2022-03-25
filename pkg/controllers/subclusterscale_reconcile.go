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
	"fmt"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SubclusterScaleReconciler will scale a VerticaDB by adding or removing subclusters.
type SubclusterScaleReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
	Vdb  *vapi.VerticaDB
}

func MakeSubclusterScaleReconciler(r *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) ReconcileActor {
	return &SubclusterScaleReconciler{VRec: r, Vas: vas, Vdb: &vapi.VerticaDB{}}
}

// Reconcile will grow/shrink the VerticaDB passed on the target pod count.
func (s *SubclusterScaleReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if s.Vas.Spec.ScalingGranularity != vapi.SubclusterScalingGranularity {
		return ctrl.Result{}, nil
	}

	if s.Vas.Spec.TargetSize == 0 {
		s.VRec.Log.Info("Target not set yet in VerticaAutoscaler")
		return ctrl.Result{}, nil
	}

	return s.scaleSubcluster(ctx)
}

// scaleSubcluster will decide to add or remove whole subclusters to reach the
// target size
func (s *SubclusterScaleReconciler) scaleSubcluster(ctx context.Context) (ctrl.Result, error) {
	var res ctrl.Result
	// Update the VerticaAutoscaler with a retry mechanism for any conflict updates
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r, e := fetchVDB(ctx, s.VRec, s.Vas, s.Vdb); verrors.IsReconcileAborted(r, e) {
			res = r
			return e
		}

		_, totSize := s.Vdb.FindSubclusterForServiceName(s.Vas.Spec.SubclusterServiceName)
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

		return s.VRec.Client.Update(ctx, s.Vdb)
	})
	return res, err
}

// considerRemovingSubclusters will shrink the Vdb by removing new subclusters.
// Changes are made in-place in s.Vdb
func (s *SubclusterScaleReconciler) considerRemovingSubclusters(podsToRemove int32) bool {
	origNumSubclusters := len(s.Vdb.Spec.Subclusters)
	for j := len(s.Vdb.Spec.Subclusters) - 1; j >= 0; j-- {
		sc := &s.Vdb.Spec.Subclusters[j]
		if sc.GetServiceName() == s.Vas.Spec.SubclusterServiceName {
			if podsToRemove > 0 && sc.Size <= podsToRemove {
				s.Vdb.Spec.Subclusters = append(s.Vdb.Spec.Subclusters[:j], s.Vdb.Spec.Subclusters[j+1:]...)
				podsToRemove -= sc.Size
				s.VRec.Log.Info("Removing subcluster to VerticaDB", "VerticaDB", s.Vdb.Name, "Subcluster", sc.Name)
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
func (s *SubclusterScaleReconciler) considerAddingSubclusters(nowPodsNeeded int32) bool {
	origSize := len(s.Vdb.Spec.Subclusters)
	scMap := s.Vdb.GenSubclusterMap()
	for nowPodsNeeded >= s.Vas.Spec.Template.Size {
		s.Vdb.Spec.Subclusters = append(s.Vdb.Spec.Subclusters, s.Vas.Spec.Template)
		sc := &s.Vdb.Spec.Subclusters[len(s.Vdb.Spec.Subclusters)-1]
		sc.Name = s.genNextSubclusterName(scMap)
		scMap[sc.Name] = sc
		nowPodsNeeded -= sc.Size
		s.VRec.Log.Info("Adding subcluster to VerticaDB", "VerticaDB", s.Vdb.Name, "Subcluster", sc.Name, "Size", sc.Size)
	}
	return origSize != len(s.Vdb.Spec.Subclusters)
}

// genNextSubclusterName will come up with a unique name to give a new subcluster
func (s *SubclusterScaleReconciler) genNextSubclusterName(scMap map[string]*vapi.Subcluster) string {
	i := 0
	for {
		// Generate a name by using the docker naming convention.  Replacing '_'
		// with '-' so that the name is valid.
		name := fmt.Sprintf("%s-%d", s.Vas.Spec.Template.Name, i)
		_, ok := scMap[name]
		if !ok {
			return name
		}
		i++
	}
}
