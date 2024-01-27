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
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	restorepointsquery "github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restorepointsquery"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

<<<<<<< HEAD
const (
	stateQuerying     = "Querying"
	stateSuccessQuery = "Query successful"
)

=======
>>>>>>> vnext
type QueryReconciler struct {
	VRec           *VerticaRestorePointsQueryReconciler
	Vrpq           *vapi.VerticaRestorePointsQuery
	Log            logr.Logger
	InitiatorPod   types.NamespacedName // The pod that we run admin commands from
	InitiatorPodIP string               // The IP of the initiating pod
	config.ConfigParamsGenerator
}

func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *vapi.VerticaRestorePointsQuery,
	log logr.Logger) controllers.ReconcileActor {
	return &QueryReconciler{
		VRec: r,
		Vrpq: vrpq,
		Log:  log.WithName("QueryReconciler"),
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: r,
			Log:  log.WithName("QueryReconciler"),
		},
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

// fetch the VerticaDB and collect access information to the communal storage for the VerticaRestorePointsQuery CR,
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}
	res := ctrl.Result{}

	opts := []restorepointsquery.Option{
		restorepointsquery.WithInitiator(q.InitiatorPod, q.InitiatorPodIP),
	}

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		var e error
		if res, e = fetchVDB(ctx, q.VRec, q.Vrpq, vdb); verrors.IsReconcileAborted(res, e) {
			return e
		}
		q.Vdb = vdb
		// build communal storage params if there is not one
		if q.ConfigurationParams == nil {
			res, e = q.ConstructConfigParms(ctx)
			if verrors.IsReconcileAborted(res, e) {
				return e
			}
		}
		// extract out the communal and config information to pass down to the vclusterops API.
		opts = append(opts,
			restorepointsquery.WithCommunalPath(vdb.GetCommunalPath()),
			restorepointsquery.WithConfigurationParams(q.ConfigurationParams.GetMap()),
		)

		return nil
	})

	return res, err
}

<<<<<<< HEAD
// runListRestorePoints will update the status condition before and after calling
=======
// setListRestorePointsQueryConditions will update the status condition before and after calling
>>>>>>> vnext
// list restore points api
// Temporarily, runListRestorePoints will not call the ListRestorePoints API
// since the dispatcher is not set up yet
func (q *QueryReconciler) runListRestorePoints(ctx context.Context, _ *ctrl.Request) error {
<<<<<<< HEAD
	// set Querying status condition and state prior to calling vclusterops API
	err := vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, "Started"), stateQuerying)
=======
	// set Querying status condition prior to calling vclusterops API
	err := vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, ""))
>>>>>>> vnext
	if err != nil {
		return err
	}

	// API should be called to proceed here
<<<<<<< HEAD
	// If we receive a failure result from the API, a state message and condition need to be updated

	// clear Querying status condition
	err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, "Completed"), stateQuerying)
=======

	// clear Querying status condition
	err = vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, ""))
>>>>>>> vnext
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
<<<<<<< HEAD
	return vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, "Completed"), stateSuccessQuery)
=======
	return vrpqstatus.UpdateCondition(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, ""))
>>>>>>> vnext
}
