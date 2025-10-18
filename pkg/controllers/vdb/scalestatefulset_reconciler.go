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
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ScaleInStatefulsetToZeroReconciler will make sure that the subclusters that are
// shut down have their pods removed.
type ScaleInStatefulsetToZeroReconciler struct {
	Rec    config.ReconcilerInterface
	Vdb    *v1.VerticaDB
	Log    logr.Logger
	PFacts *podfacts.PodFacts
}

func MakeScaleInStatefulsetToZeroReconciler(r config.ReconcilerInterface,
	vdb *v1.VerticaDB, pfacts *podfacts.PodFacts, log logr.Logger) controllers.ReconcileActor {
	return &ScaleInStatefulsetToZeroReconciler{
		Rec:    r,
		Vdb:    vdb,
		PFacts: pfacts,
		Log:    log.WithName("ScaleInStatefulsetToZeroReconciler"),
	}
}

func (s *ScaleInStatefulsetToZeroReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if s.Vdb.IsStatusConditionTrue(v1.MainClusterPodsTerminated) {
		// Already done.
		return ctrl.Result{}, nil
	}
	scMap := s.Vdb.GenSubclusterMap()
	scStatusMap := s.Vdb.GenSubclusterStatusMap()
	finder := iter.MakeSubclusterFinder(s.Rec.GetClient(), s.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindInVdb, s.PFacts.SandboxName)
	if err != nil {
		return ctrl.Result{}, err
	}
	var scaledStsCount int
	for inx := range stss.Items {
		sts := &stss.Items[inx]
		sc := scMap[sts.Labels[vmeta.SubclusterNameLabel]]
		scStatus := scStatusMap[sts.Labels[vmeta.SubclusterNameLabel]]
		if sc == nil || scStatus == nil {
			// We just continue. If there is any issue with a subcluster, there are
			// other reconcilers that will catch it.
			continue
		}
		if !sc.Shutdown || !scStatus.Shutdown {
			// Nothing to do if the subcluster is not shutdown.
			continue
		}
		scaled, err := s.scaleStsToZero(ctx, sts, sc)
		if err != nil {
			return ctrl.Result{}, err
		}
		if scaled {
			scaledStsCount++
		}
	}
	mainScCount := len(s.Vdb.GetSubclustersInSandbox(v1.MainCluster))
	if s.PFacts.SandboxName == v1.MainCluster && scaledStsCount == mainScCount {
		s.Log.Info("All statefulsets in the main cluster have been scaled to zero")
		err := vdbstatus.UpdateCondition(ctx, s.Rec.GetClient(), s.Vdb, v1.MakeCondition(
			v1.MainClusterPodsTerminated,
			metav1.ConditionTrue,
			"AllStatefulsetsScaledToZero",
		))
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

func (s *ScaleInStatefulsetToZeroReconciler) scaleStsToZero(ctx context.Context, sts *appsv1.StatefulSet,
	sc *v1.Subcluster) (bool, error) {
	scaledToZero := false
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		nm := names.GenNamespacedName(s.Vdb, sts.Name)
		err := s.Rec.GetClient().Get(ctx, nm, sts)
		if err != nil {
			return err
		}

		oldSize := sts.Spec.Replicas
		newSize := sc.GetStsSize(s.Vdb)
		scaledToZero = newSize == 0
		// No need to update if the size is already correct or if we are not
		// scaling to zero.
		if *oldSize == newSize || newSize != 0 {
			return nil
		}
		msg := fmt.Sprintf("Terminating all pods of subcluster %s in %s", sc.Name, s.PFacts.GetClusterExtendedName())
		s.Rec.Event(s.Vdb, corev1.EventTypeNormal, events.TerminatingSubclusterPods, msg)
		// Update the StatefulSet to the new size.
		sts.Spec.Replicas = &newSize
		return s.Rec.GetClient().Update(ctx, sts)
	})
	return scaledToZero, err
}
