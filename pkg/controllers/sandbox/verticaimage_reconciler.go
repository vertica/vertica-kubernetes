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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// VerticaImageReconciler will recreate the secondary subclusters' StatefulSets
// in main cluster if those subclusters have a different vertica image than
// the one in primary subclusters
type VerticaImageReconciler struct {
	SRec   *SandboxConfigMapReconciler
	Vdb    *vapi.VerticaDB
	Log    logr.Logger
	PFacts *vdbcontroller.PodFacts
}

func MakeVerticaImageReconciler(r *SandboxConfigMapReconciler, vdb *vapi.VerticaDB,
	log logr.Logger, pfacts *vdbcontroller.PodFacts) controllers.ReconcileActor {
	pfactsForMainCluster := pfacts.Copy(vapi.MainCluster)
	return &VerticaImageReconciler{
		SRec:   r,
		Log:    log.WithName("VerticaImageReconciler"),
		Vdb:    vdb,
		PFacts: &pfactsForMainCluster,
	}
}

// Reconcile will fix the image of unsandboxed subclusters by recreating their StatefulSets
func (r *VerticaImageReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db
	if r.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly || !r.Vdb.IsEON() {
		return ctrl.Result{}, nil
	}

	// collect pod facts for main cluster
	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return r.reconcileVerticaImage(ctx)
}

// reconcileVerticaImage recreates the StatefulSet of the secondary subclusters that
// have the wrong vertica image
func (r *VerticaImageReconciler) reconcileVerticaImage(ctx context.Context) (ctrl.Result, error) {
	scsWithWrongImage := r.PFacts.FindSecondarySubclustersWithDifferentImage()
	scMap := r.Vdb.GenSubclusterMap()
	for _, sc := range scsWithWrongImage {
		scInVdb, ok := scMap[sc]
		if !ok {
			r.Log.Info("Pods' subcluster name cannot be found in vdb", "subclusterName", sc)
			continue
		}
		nm := names.GenStsName(r.Vdb, scInVdb)
		curSts := &appsv1.StatefulSet{}
		expSts := builder.BuildStsSpec(nm, r.Vdb, scInVdb)
		err := r.SRec.Client.Get(ctx, nm, curSts)
		if err != nil && kerrors.IsNotFound(err) {
			r.Log.Info("Creating statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", expSts.Spec.Template.Spec.Containers[0].Image)
			return ctrl.Result{}, r.createSts(ctx, expSts)
		}
		err = r.recreateSts(ctx, curSts, expSts)
		if err != nil {
			r.Log.Error(err, "failed to recreate statefulset", "Name", nm)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// recreateSts will drop then create the statefulset
func (r *VerticaImageReconciler) recreateSts(ctx context.Context, curSts, expSts *appsv1.StatefulSet) error {
	if err := r.SRec.Client.Delete(ctx, curSts); err != nil {
		return err
	}
	return r.createSts(ctx, expSts)
}

// createSts will create a new sts. It assumes the statefulset doesn't already exist.
func (r *VerticaImageReconciler) createSts(ctx context.Context, expSts *appsv1.StatefulSet) error {
	err := ctrl.SetControllerReference(r.Vdb, expSts, r.SRec.Client.Scheme())
	if err != nil {
		return err
	}
	return r.SRec.GetClient().Create(ctx, expSts)
}
