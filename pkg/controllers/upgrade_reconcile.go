/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeReconciler will update the status field of the vdb.
type UpgradeReconciler struct {
	VRec   *VerticaDBReconciler
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *PodFacts
}

// MakeUpgradeReconciler will build an UpgradeReconciler object
func MakeUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts) ReconcileActor {
	return &UpgradeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PFacts: pfacts}
}

// Reconcile will update the status of the Vdb based on the pod facts
func (u *UpgradeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := u.PFacts.Collect(ctx, u.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
