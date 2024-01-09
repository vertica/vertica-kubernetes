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
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type QueryReconciler struct {
	VRec       *VerticaRestorePointsQueryReconciler
	Vrpq       *vapi.VerticaRestorePointsQuery
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
}

func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &QueryReconciler{
		VRec:       r,
		Vrpq:       vrpq,
		Log:        log,
		Dispatcher: dispatcher,
	}
}

func (q *QueryReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryComplete is true
	isSet := q.Vrpq.IsStatusConditionTrue(vapi.Querying)
	if isSet {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, q.setListRestorePointsQueryConditions(ctx, req)
}

// setListRestorePointsQueryConditions will update the status condition before and after calling
// list restore points api
func (q *QueryReconciler) setListRestorePointsQueryConditions(ctx context.Context, _ *ctrl.Request) error {
	// set Querying status condition prior to calling vclusterops API
	err := vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, ""))
	if err != nil {
		return err
	}

	errAPI := q.Dispatcher.ListRestorePoints(ctx)
	// Include an error message in the status condition if the API fails.
	if errAPI != nil {
		q.VRec.Log.Info("Fail to call Vcluster list restore points API")
		return errAPI
	}

	// clear Querying status condition after calling API
	err = vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionUnknown, ""))
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
	return vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, ""))
}
