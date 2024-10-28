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

package sandbox

import (
	"context"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ScaleStafulsetReconciler will make sure that the sandbox's subclusters that are
// shut down have their pods removed.
type ScaleStafulsetReconciler struct {
	VRec   *SandboxConfigMapReconciler
	Vdb    *v1.VerticaDB
	Log    logr.Logger
	PFacts *podfacts.PodFacts
}

func MakeScaleStafulsetReconciler(r *SandboxConfigMapReconciler,
	vdb *v1.VerticaDB, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &ScaleStafulsetReconciler{
		VRec:   r,
		Vdb:    vdb,
		PFacts: pfacts,
	}
}

func (s *ScaleStafulsetReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	scMap := s.Vdb.GenSubclusterMap()
	finder := iter.MakeSubclusterFinder(s.VRec.GetClient(), s.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindInVdb, s.PFacts.SandboxName)
	if err != nil {
		return ctrl.Result{}, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			nm := names.GenNamespacedName(s.Vdb, sts.Name)
			err := s.VRec.GetClient().Get(ctx, nm, sts)
			if err != nil {
				return err
			}
			oldSize := sts.Spec.Replicas
			sc := scMap[sts.Labels[vmeta.SubclusterNameLabel]]
			if sc == nil {
				return nil
			}
			newSize := sc.GetStsSize(s.Vdb)
			if *oldSize == newSize {
				return nil
			}
			sts.Spec.Replicas = &newSize
			return s.VRec.GetClient().Update(ctx, sts)
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
