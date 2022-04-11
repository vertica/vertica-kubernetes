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

package vas

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/vasstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

type TargetSizeInitializerReconciler struct {
	VRec *VerticaAutoscalerReconciler
	Vas  *vapi.VerticaAutoscaler
}

func MakeTargetSizeInitializerReconciler(v *VerticaAutoscalerReconciler, vas *vapi.VerticaAutoscaler) controllers.ReconcileActor {
	return &TargetSizeInitializerReconciler{VRec: v, Vas: vas}
}

// Reconcile will update the targetSize in a Vas if not already initialized
func (v *TargetSizeInitializerReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if len(v.Vas.Status.Conditions) > vapi.TargetSizeInitializedIndex &&
		v.Vas.Status.Conditions[vapi.TargetSizeInitializedIndex].Status == corev1.ConditionTrue {
		// Already initialized
		return ctrl.Result{}, nil
	}

	if v.Vas.Spec.TargetSize == 0 {
		if res, err := v.initTargetSize(ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, v.setTargetSizeInitializedCondition(ctx, req)
}

// initTargetSize will calculate what the targetSize is based on the current vdb.
func (v *TargetSizeInitializerReconciler) initTargetSize(ctx context.Context) (ctrl.Result, error) {
	vdb := &vapi.VerticaDB{}
	res := ctrl.Result{}

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var e error
		if res, e = fetchVDB(ctx, v.VRec, v.Vas, vdb); verrors.IsReconcileAborted(res, e) {
			return e
		}
		_, totSize := vdb.FindSubclusterForServiceName(v.Vas.Spec.ServiceName)

		if v.Vas.Spec.TargetSize != totSize {
			v.VRec.Log.Info("Updating targetSize in vas", "targetSize", totSize)
			v.Vas.Spec.TargetSize = totSize
			return v.VRec.Client.Update(ctx, v.Vas)
		}
		return nil
	})
	return res, err
}

// setTargetSizeInitializedCondition will seth the targetSizeInitialized condition to true
func (v *TargetSizeInitializerReconciler) setTargetSizeInitializedCondition(ctx context.Context, req *ctrl.Request) error {
	cond := vapi.VerticaAutoscalerCondition{
		Type:   vapi.TargetSizeInitialized,
		Status: corev1.ConditionTrue,
	}
	return vasstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, req, cond)
}
