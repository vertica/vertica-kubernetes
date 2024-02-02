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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	restorepointsquery "github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restorepoints"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	stateQuerying     = "Querying"
	stateSuccessQuery = "Query successful"
	stateFailedQuery  = "Query failed"
	podRunning        = "Running"
)

type QueryReconciler struct {
	VRec *VerticaRestorePointsQueryReconciler
	Vrpq *vapi.VerticaRestorePointsQuery
	Log  logr.Logger
	config.ConfigParamsGenerator
	OpCfg opcfg.OperatorConfig
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

func (q *QueryReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if QueryComplete is true
	isSet := q.Vrpq.IsStatusConditionTrue(vapi.QueryComplete)
	if isSet {
		return ctrl.Result{}, nil
	}
	// collect information from a VerticaDB.
	if res, err := q.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	finder := iter.MakeSubclusterFinder(q.VRec.Client, q.Vdb)
	pods, err := finder.FindPods(ctx, iter.FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}

	// find a pod to execute the vclusterops API
	var podIP string
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == podRunning {
			for j := range pod.Status.ContainerStatuses {
				// check NMA container ready
				if pod.Status.ContainerStatuses[j].Ready {
					podIP = pod.Status.PodIP
					break
				}
			}
		}
		if podIP != "" {
			break
		}
	}

	// setup dispatcher for vclusterops API
	dispatcher, err := q.makeDispatcher(q.Log, q.Vdb, nil)
	if err != nil {
		return ctrl.Result{}, err
	}

	// extract out the communal and config information to pass down to the vclusterops API.
	opts := []restorepointsquery.Option{}
	opts = append(opts,
		restorepointsquery.WithInitiator(q.Vrpq.ExtractNamespacedName(), podIP),
		restorepointsquery.WithCommunalPath(q.Vdb.GetCommunalPath()),
		restorepointsquery.WithConfigurationParams(q.ConfigurationParams.GetMap()),
	)
	return ctrl.Result{}, q.runShowRestorePoints(ctx, dispatcher, opts)
}

// fetch the VerticaDB and collect access information to the communal storage for the VerticaRestorePointsQuery CR
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}
	var res ctrl.Result
	var err error
	if res, err = fetchVDB(ctx, q.VRec, q.Vrpq, vdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	q.Vdb = vdb

	// Build communal storage params if there is not one
	if q.ConfigurationParams == nil {
		res, err = q.ConstructConfigParms(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return res, err
}

// runShowRestorePoints call the vclusterOps API to get the restore points
func (q *QueryReconciler) runShowRestorePoints(ctx context.Context, dispatcher vadmin.Dispatcher,
	opts []restorepointsquery.Option) (err error) {
	// set Querying status condition and state prior to calling vclusterops API
	err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionTrue, "Started"), stateQuerying)
	if err != nil {
		return err
	}

	// call showRestorePoints vcluster API
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsStarted,
		"Starting show restore points")
	start := time.Now()
	if res, errRun := dispatcher.ShowRestorePoints(ctx, opts...); verrors.IsReconcileAborted(res, errRun) {
		if errRun != nil {
			err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
				v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, "Failed"), stateFailedQuery)
			if err != nil {
				errRun = errors.Join(errRun, err)
			}
			return errRun
		}
	}
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsSucceeded,
		"Successfully queried restore points in %s", time.Since(start).Truncate(time.Second))

	// set the QueryComplete if the vclusterops API succeeded
	return vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, "Completed"), stateSuccessQuery)
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (q *QueryReconciler) makeDispatcher(log logr.Logger, vdb *v1.VerticaDB,
	passwd *string) (vadmin.Dispatcher, error) {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		var password string
		if passwd != nil {
			password = *passwd
		}
		return vadmin.MakeVClusterOps(log, vdb, q.VRec.GetClient(), password, q.VRec, vadmin.SetupVClusterOps), nil
	}
	return nil, fmt.Errorf("ShowRestorePoints is not supported for admintools deployments")
}
