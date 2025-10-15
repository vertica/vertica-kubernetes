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

package vdb

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ScaleStafulsetReconciler will make sure that the subclusters that are
// shut down have their pods removed.
type ScaleStafulsetReconciler struct {
	Rec    config.ReconcilerInterface
	Vdb    *v1.VerticaDB
	Log    logr.Logger
	PFacts *podfacts.PodFacts
}

func MakeScaleStafulsetReconciler(r config.ReconcilerInterface,
	vdb *v1.VerticaDB, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &ScaleStafulsetReconciler{
		Rec:    r,
		Vdb:    vdb,
		PFacts: pfacts,
	}
}

func (s *ScaleStafulsetReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	scMap := s.Vdb.GenSubclusterMap()
	finder := iter.MakeSubclusterFinder(s.Rec.GetClient(), s.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindInVdb, s.PFacts.SandboxName)
	if err != nil {
		return ctrl.Result{}, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]
		err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			nm := names.GenNamespacedName(s.Vdb, sts.Name)
			err := s.Rec.GetClient().Get(ctx, nm, sts)
			if err != nil {
				return err
			}
			oldSize := sts.Spec.Replicas
			sc := scMap[sts.Labels[vmeta.SubclusterNameLabel]]
			if sc == nil {
				return fmt.Errorf("subcluster %s not found in vdb", sts.Labels[vmeta.SubclusterNameLabel])
			}
			newSize := sc.GetStsSize(s.Vdb)
			if *oldSize == newSize {
				return nil
			}
			msg := ""
			if newSize == 0 {
				msg = fmt.Sprintf("Terminating all pods of subcluster %s in %s", sc.Name, s.PFacts.GetClusterExtendedName())
			} else {
				msg = fmt.Sprintf("Restarting all pods of subcluster %s in %s", sc.Name, s.PFacts.GetClusterExtendedName())
			}
			s.Rec.Event(s.Vdb, corev1.EventTypeNormal, events.ScalingSubclusterPods, msg)
			// Update the StatefulSet to the new size.
			sts.Spec.Replicas = &newSize
			return s.Rec.GetClient().Update(ctx, sts)
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}
