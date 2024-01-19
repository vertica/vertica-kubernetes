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
	vdbconfig "github.com/vertica/vertica-kubernetes/pkg/controllers/vdbconfig"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

type QueryReconciler struct {
	VRec *VerticaRestorePointsQueryReconciler
	Vrpq *vapi.VerticaRestorePointsQuery
	Log  logr.Logger
	vdbconfig.ConfigParamsGenerator
}

func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger) controllers.ReconcileActor {
	return &QueryReconciler{
		VRec: r,
		Vrpq: vrpq,
		Log:  log.WithName("QueryReconciler"),
	}
}

func (q *QueryReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryComplete is true
	isSet := q.Vrpq.IsStatusConditionTrue(vapi.Querying)
	if isSet {
		return ctrl.Result{}, nil
	}
	// collect information from a VerticaDB.
	if res, err := q.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, q.runListRestorePoints(ctx, req)
}

func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &vapi.VerticaDB{}
	res := ctrl.Result{}

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var e error
		if res, e = fetchVDB(ctx, q.VRec, q.Vrpq, vdb); verrors.IsReconcileAborted(res, e) {
			return e
		}
		// If a communal path is set, include all of the EON parameters.
		if vdb.Spec.Communal.Path != "" {
			// build communal storage params if there is not one
			if q.ConfigurationParams == nil {
				res, e = q.ConstructConfigParms(ctx)
				if verrors.IsReconcileAborted(res, e) {
					return e
				}
			}
		}
		return nil
	})

	return res, err
}

// setListRestorePointsQueryConditions will update the status condition before and after calling
// list restore points api
// Temporarily, runListRestorePoints will not call the ListRestorePoints API
// since the dispatcher is not set up yet
func (q *QueryReconciler) runListRestorePoints(ctx context.Context, _ *ctrl.Request) error {
	// set Querying status condition prior to calling vclusterops API
	err := vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, ""))
	if err != nil {
		return err
	}

	// API should be called to proceed here

	// clear Querying status condition
	err = vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, ""))
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
	return vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, ""))
}
