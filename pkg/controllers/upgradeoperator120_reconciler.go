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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeOperator120Reconciler will handle any upgrade actions for k8s
// objects created in 1.2.0 or prior.
type UpgradeOperator120Reconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
}

// MakeUpgradeOperator120Reconciler will build a UpgradeOperatorFrom120Reconciler object
func MakeUpgradeOperator120Reconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) ReconcileActor {
	return &UpgradeOperator120Reconciler{VRec: vdbrecon, Log: log, Vdb: vdb}
}

// Reconcile will handle any upgrade actions for k8s objects created in 1.2.0 or prior.
func (u *UpgradeOperator120Reconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	finder := MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}

	// In 1.3.0, we changed the selector labels for statefulsets.  But selector
	// labels are immutable.  So to upgrade to this current version we need to
	// delete any sts created in prior releases.
	for i := range stss.Items {
		sts := &stss.Items[i]
		opVer, ok := sts.ObjectMeta.Labels[OperatorVersionLabel]
		if !ok {
			continue
		}
		switch opVer {
		case OperatorVersion120, OperatorVersion110, OperatorVersion100:
			u.VRec.EVRec.Event(u.Vdb, corev1.EventTypeNormal, events.OperatorUpgrade,
				fmt.Sprintf("Deleting statefulset '%s' because it was created by an old operator (pre-%s)",
					sts.Name, OperatorVersion130))
			if err := u.VRec.Client.Delete(ctx, sts); err != nil {
				u.Log.Info("Error deleting old statefulset", "opVer", opVer)
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}
