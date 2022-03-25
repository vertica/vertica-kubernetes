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
	"reflect"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

type VASStatusReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
}

// MakeVASStatusReconciler will create a VASStatusReconciler object and return it
func MakeVASStatusReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) ReconcileActor {
	return &VASStatusReconciler{VRec: v, Vas: vas}
}

// Reconcile will handle updating the status portion of a VerticaAutoscaler
func (v *VASStatusReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result
	vdb := &vapi.VerticaDB{}

	// Try the status update in a retry loop to handle the case where someone
	// update the VerticaAutoscaler since we last fetched.
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if r, e := fetchVDB(ctx, v.VRec, v.Vas, vdb); verrors.IsReconcileAborted(r, e) {
			res = r
			return e
		}

		// We will calculate the status for the vas object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vasOrig := v.Vas.DeepCopy()

		v.Vas.Status.Selector = fmt.Sprintf("%s=%s", builder.SubclusterSvcNameLabel, v.Vas.Spec.SubclusterServiceName)

		_, totSize := vdb.FindSubclusterForServiceName(v.Vas.Spec.SubclusterServiceName)
		v.Vas.Status.Size = totSize

		if !reflect.DeepEqual(vasOrig, v.Vas.Status) {
			v.VRec.Log.Info("Updating vas status", "status", v.Vas.Status)
			if err := v.VRec.Client.Status().Update(ctx, v.Vas); err != nil {
				return err
			}
		}
		return nil
	})

	return res, err
}
