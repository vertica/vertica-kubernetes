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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeOperatorReconciler will handle any upgrade actions for k8s
// objects created in older operators.
type UpgradeOperatorReconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
}

// MakeUpgradeOperatorReconciler will build a UpgradeOperatorFromReconciler object
func MakeUpgradeOperatorReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &UpgradeOperatorReconciler{VRec: vdbrecon, Log: log.WithName("UpgradeOperatorReconciler"), Vdb: vdb}
}

// Reconcile will handle any upgrade actions for k8s objects created in older operators.
func (u *UpgradeOperatorReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	finder := iter.MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindExisting, vapi.MainCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	// In 2.2.0, we changed the selector labels for statefulsets.  But selector
	// labels are immutable.  So to upgrade to this current version we need to
	// delete any sts created in prior releases.
	for i := range stss.Items {
		sts := &stss.Items[i]
		opVer, ok := sts.ObjectMeta.Labels[vmeta.OperatorVersionLabel]
		if !ok {
			u.Log.Info("skipping object since we could not find operator version label", "name", sts.Name)
			continue
		}
		if opVer < vmeta.OperatorVersion220 {
			u.VRec.Event(u.Vdb, corev1.EventTypeNormal, events.OperatorUpgrade,
				fmt.Sprintf("Deleting statefulset '%s' because it was created by an older version of the operator (%s)",
					sts.Name, opVer))
			if err := u.VRec.Client.Delete(ctx, sts); err != nil {
				u.Log.Info("Error deleting old statefulset", "opVer", opVer)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}
