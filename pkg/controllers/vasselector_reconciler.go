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
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

type SelectorReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
}

// MakeSelectorReconciler will create a SelectorReconciler object and return it
func MakeSelectorReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) ReconcileActor {
	return &SelectorReconciler{VRec: v, Vas: vas}
}

// Reconcile will handle updating the selector in the status portion of a VerticaAutoscaler
func (v *SelectorReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, vasstatus.SetSelector(ctx, v.VRec.Client, v.VRec.Log, req)
}
