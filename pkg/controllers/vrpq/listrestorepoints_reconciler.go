/*
 (c) Copyright [2021-2023] Open Text.
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

package vrpq

import (
	"context"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ListRestorePointsQueryReconciler struct {
	VRec       *VerticaRestorePointsQueryReconciler
	Vrpq       *vapi.VerticaRestorePointsQuery
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
}

func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger) controllers.ReconcileActor {
	return &ListRestorePointsQueryReconciler{
		VRec: r,
		Vrpq: vrpq,
		Log:  log,
	}
}

func (v *ListRestorePointsQueryReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryComplete is true
	if len(v.Vrpq.Status.Conditions) > vapi.QueryCompleteIndex &&
		v.Vrpq.Status.Conditions[vapi.QueryCompleteIndex].Status == corev1.ConditionTrue {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, v.setListRestorePointsQueryConditions(ctx, req)
}

// setListRestorePointsQueryConditions will update the status condition before and after calling
// list restore points api
func (v *ListRestorePointsQueryReconciler) setListRestorePointsQueryConditions(ctx context.Context, _ *ctrl.Request) error {
	// set Querying status condition prior to calling vclusterops API
	err := vrpqstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, v.Vrpq,
		vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionTrue})
	if err != nil {
		return err
	}

	errAPI := v.Dispatcher.ListRestorePoints(ctx)
	// Include an error message in the status condition if the API fails.
	if errAPI != nil {
		v.VRec.Log.Info("Fail to call Vcluster list restore points API")
		// set the Querying to false if the API fails
		err = vrpqstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, v.Vrpq,
			vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionFalse})
		if err != nil {
			return err
		}
		return errAPI
	}

	// clear Querying status condition after calling API
	err = vrpqstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, v.Vrpq,
		vapi.VerticaRestorePointsQueryCondition{Type: vapi.Querying, Status: corev1.ConditionUnknown})
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
	cond := vapi.VerticaRestorePointsQueryCondition{
		Type:   vapi.QueryComplete,
		Status: corev1.ConditionTrue,
	}
	return vrpqstatus.UpdateCondition(ctx, v.VRec.Client, v.VRec.Log, v.Vrpq, cond)
}
