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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	restorepointsquery "github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restorepoints"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	stateQuerying     = "Querying"
	stateSuccessQuery = "Query successful"
	stateFailedQuery  = "Query failed"
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

func (q *QueryReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
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
		if i != names.GetNMAContainerIndex() {
			continue
		}
		pod := &pods.Items[i]
		podIP = pod.Status.PodIP
		break
	}

	// setup dispatcher for vclusterops API
	passwd, err := q.GetSuperuserPassword(ctx, q.Log, q.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	prunner := cmds.MakeClusterPodRunner(q.Log, q.VRec.Cfg, q.Vdb.GetVerticaUser(), passwd)
	dispatcher := q.makeDispatcher(q.Log, q.Vdb, prunner, passwd)

	// extract out the communal and config information to pass down to the vclusterops API.
	opts := []restorepointsquery.Option{}
	opts = append(opts,
		restorepointsquery.WithInitiator(q.Vrpq.ExtractNamespacedName(), podIP),
		restorepointsquery.WithCommunalPath(q.Vdb.GetCommunalPath()),
		restorepointsquery.WithConfigurationParams(q.ConfigurationParams.GetMap()),
	)
	return ctrl.Result{}, q.runShowRestorePoints(ctx, req, dispatcher, opts)
}

// fetch the VerticaDB and collect access information to the communal storage for the VerticaRestorePointsQuery CR,
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (ctrl.Result, error) {
	vdb := &v1.VerticaDB{}
	res := ctrl.Result{}
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
		return nil
	})

	return res, err
}

// runShowRestorePoints will update the status condition and state before and after calling
// showrestorepoints vclusterops api
func (q *QueryReconciler) runShowRestorePoints(ctx context.Context, _ *ctrl.Request, dispatcher vadmin.Dispatcher,
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
				return err
			}
		}
	}
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsSucceeded,
		"Successfully showed restore point with database '%s'. It took %s", q.Vdb.Spec.DBName, time.Since(start))

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

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (q *QueryReconciler) makeDispatcher(log logr.Logger, vdb *v1.VerticaDB, prunner cmds.PodRunner,
	passwd string) vadmin.Dispatcher {
	if vmeta.UseVClusterOps(vdb.Annotations) {
		return vadmin.MakeVClusterOps(log, vdb, q.VRec.GetClient(), passwd, q.VRec, vadmin.SetupVClusterOps)
	}
	return vadmin.MakeAdmintools(log, vdb, prunner, q.VRec, q.OpCfg.DevMode)
}

// GetSuperuserPassword returns the superuser password if it has been provided
func (q *QueryReconciler) GetSuperuserPassword(ctx context.Context, log logr.Logger,
	vdb *v1.VerticaDB) (string, error) {
	if vdb.Spec.PasswordSecret == "" {
		return "", nil
	}

	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   q.VRec.GetClient(),
		Log:      log,
		VDB:      vdb,
		EVWriter: q.VRec,
	}
	secret, err := fetcher.Fetch(ctx, names.GenSUPasswdSecretName(vdb))
	if err != nil {
		return "", err
	}

	pwd, ok := secret[builder.SuperuserPasswordKey]
	if !ok {
		return "", fmt.Errorf("password not found, secret must have a key with name '%s'", builder.SuperuserPasswordKey)
	}
	return string(pwd), nil
}
