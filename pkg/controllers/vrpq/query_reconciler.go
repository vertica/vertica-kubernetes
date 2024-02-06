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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
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

	// setup dispatcher for vclusterops API
	dispatcher, err := q.makeDispatcher(q.Log, q.Vdb, nil /*password*/)
	if err != nil {
		return ctrl.Result{}, err
	}

	finder := iter.MakeSubclusterFinder(q.VRec.Client, q.Vdb)
	pods, err := finder.FindPods(ctx, iter.FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}

	// find a pod to execute the vclusterops API
	podIP, res := q.findRunningPodWithNMAContainer(pods)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// extract out the communal and config information to pass down to the vclusterops API.
	opts := []showrestorepoints.Option{}
	opts = append(opts,
		showrestorepoints.WithInitiator(q.Vrpq.ExtractNamespacedName(), podIP),
		showrestorepoints.WithCommunalPath(q.Vdb.GetCommunalPath()),
		showrestorepoints.WithConfigurationParams(q.ConfigurationParams.GetMap()),
	)
	return ctrl.Result{}, q.runShowRestorePoints(ctx, dispatcher, opts)
}

// fetch the VerticaDB and collect access information to the communal storage for the VerticaRestorePointsQuery CR
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (res ctrl.Result, err error) {
	vdb := &v1.VerticaDB{}
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
	opts []showrestorepoints.Option) (err error) {
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
	restorePoints, errRun := dispatcher.ShowRestorePoints(ctx, opts...)
	if errRun != nil {
		q.VRec.Event(q.Vrpq, corev1.EventTypeWarning, events.ShowRestorePointsFailed, "Failed when calling show restore points")
		err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, "Failed"), stateFailedQuery)
		if err != nil {
			errRun = errors.Join(errRun, err)
		}
		return errRun
	}
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsSucceeded,
		"Successfully queried restore points in %s", time.Since(start).Truncate(time.Second))

	err = vrpqstatus.UpdateRestorePointStatus(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		restorePoints)
	if err != nil {
		return err
	}

	// clear Querying status condition
	err = vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.Querying, metav1.ConditionFalse, "Completed"), stateQuerying)
	if err != nil {
		return err
	}

	// set the QueryComplete if the vclusterops API succeeded
	return vrpqstatus.UpdateConditionAndState(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		v1.MakeCondition(vapi.QueryComplete, metav1.ConditionTrue, "Completed"), stateSuccessQuery)
}

// findRunningPodWithNMAContainer finds a pod to execute the vclusterops API.
// The pod should be running and the NMA container should be ready
func (q *QueryReconciler) findRunningPodWithNMAContainer(pods *corev1.PodList) (string, ctrl.Result) {
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Status.Phase == podRunning {
			for j := range pod.Status.ContainerStatuses {
				if pod.Status.ContainerStatuses[j].Ready && pod.Status.ContainerStatuses[j].Name == names.NMAContainer {
					return pod.Status.PodIP, ctrl.Result{}
				}
			}
		}
	}
	q.Log.Info("couldn't find any pod to run the query")
	return "", ctrl.Result{Requeue: true}
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (q *QueryReconciler) makeDispatcher(log logr.Logger, vdb *v1.VerticaDB,
	_ *string) (vadmin.Dispatcher, error) {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		// The password isn't needed since our API is going to strictly communicate with the NMA
		return vadmin.MakeVClusterOps(log, vdb, q.VRec.GetClient(), "", q.VRec, vadmin.SetupVClusterOps), nil
	}
	return nil, fmt.Errorf("ShowRestorePoints is not supported for admintools deployments")
}
