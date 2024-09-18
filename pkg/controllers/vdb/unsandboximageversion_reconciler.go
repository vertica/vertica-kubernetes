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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UnsandboxImageVersion will recreate the secondary subclusters' StatefulSets
// in main cluster if those subclusters have a different vertica image than
// the one in primary subclusters
type UnsandboxImageVersion struct {
	VRec   *VerticaDBReconciler
	Vdb    *vapi.VerticaDB
	Log    logr.Logger
	PFacts *PodFacts
}

func MakeUnsandboxImageVersionReconciler(r *VerticaDBReconciler, vdb *vapi.VerticaDB,
	log logr.Logger, pfacts *PodFacts) controllers.ReconcileActor {
	return &UnsandboxImageVersion{
		VRec:   r,
		Log:    log.WithName("UnsandboxImageVersion"),
		Vdb:    vdb,
		PFacts: pfacts,
	}
}

// Reconcile will fix the image of unsandboxed subclusters by recreating their StatefulSets
func (r *UnsandboxImageVersion) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db
	if r.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly || !r.Vdb.IsEON() {
		return ctrl.Result{}, nil
	}

	// we do not want to recreate statefulSets during an upgrade
	if r.Vdb.IsUpgradeInProgress() {
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
func (r *UnsandboxImageVersion) reconcileVerticaImage(ctx context.Context) (ctrl.Result, error) {
	scsWithWrongImage, priScImage := r.PFacts.FindSecondarySubclustersWithDifferentImage()
	if len(scsWithWrongImage) == 0 {
		return ctrl.Result{}, nil
	}
	if priScImage == "" {
		r.Log.Info("Cannot get primary subcluster's image from podfacts")
		return ctrl.Result{}, nil
	}
	scMap := r.Vdb.GenSubclusterMap()
	recreatedSts := false
	for _, sc := range scsWithWrongImage {
		scInVdb, ok := scMap[sc]
		if !ok {
			r.Log.Info("Pods' subcluster name cannot be found in vdb", "subclusterName", sc)
			continue
		}
		nm := names.GenStsName(r.Vdb, scInVdb)
		curSts := &appsv1.StatefulSet{}
		expSts := builder.BuildStsSpec(nm, r.Vdb, scInVdb)
		err := vk8s.SetVerticaImage(expSts.Spec.Template.Spec.Containers, priScImage, r.Vdb.IsNMASideCarDeploymentEnabled())
		if err != nil {
			return ctrl.Result{}, err
		}
		err = r.VRec.Client.Get(ctx, nm, curSts)
		if err != nil && kerrors.IsNotFound(err) {
			r.Log.Info("Creating statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", priScImage)
			err = createSts(ctx, r.VRec, expSts, r.Vdb)
		} else {
			r.Log.Info("Recreating statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", priScImage)
			err = recreateSts(ctx, r.VRec, curSts, expSts, r.Vdb)
		}
		if err != nil {
			r.Log.Error(err, "failed to recreate statefulset", "Name", nm)
			return ctrl.Result{}, err
		}
		recreatedSts = true
		r.Log.Info("Successfully recreated statefulset", "Name", nm, "Size", expSts.Spec.Replicas, "Image", priScImage)
	}
	if recreatedSts {
		r.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}
