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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

type VASStatusReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
}

func MakeVASStatusReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) ReconcileActor {
	return &VASStatusReconciler{VRec: v, Vas: vas}
}

// Reconcile will handle updating the currentSize in the status field
func (v *VASStatusReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	vdb := &vapi.VerticaDB{}
	if res, err := fetchVDB(ctx, v.VRec, v.Vas, vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	_, totSize := vdb.FindSubclusterForServiceName(v.Vas.Spec.SubclusterServiceName)
	return ctrl.Result{}, vasstatus.RefreshCurrentSize(ctx, v.VRec.Client, v.VRec.Log, req, totSize)
}
