/*
 (c) Copyright [2021-2024] Open Text.
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

package vrep

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	statusStarted   = "started"
	statusFailed    = "failed"
	statusCompleted = "completed"

	opLoadSnapshotPrep = "load_snapshot_prep"
	opDataTransfer     = "data_transfer"
	opLoadSnapshot     = "load_snapshot"
)

type ReplicationStatusReconciler struct {
	client.Client
	VRec         *VerticaReplicatorReconciler
	Vrep         *v1beta1.VerticaReplicator
	dispatcher   vadmin.Dispatcher
	SourcePFacts *podfacts.PodFacts
	TargetPFacts *podfacts.PodFacts
	Log          logr.Logger
	TargetInfo   *ReplicationInfo
}

func MakeReplicationStatusReconciler(cli client.Client, r *VerticaReplicatorReconciler, vrep *v1beta1.VerticaReplicator,
	log logr.Logger) controllers.ReconcileActor {
	return &ReplicationStatusReconciler{
		Client:     cli,
		VRec:       r,
		Vrep:       vrep,
		Log:        log.WithName("ReplicationStatusReconciler"),
		TargetInfo: &ReplicationInfo{},
	}
}

func (r *ReplicationStatusReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op if replication is not being done asynchronously
	if r.Vrep.Spec.Mode != replicationModeAsync {
		r.Log.Info("Stopped reconciling status: not async")
		return ctrl.Result{}, nil
	}

	// no-op if ReplicationComplete is present (either true or false)
	isPresent := r.Vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)
	if isPresent {
		r.Log.Info("Stopped reconciling status: complete")
		return ctrl.Result{}, nil
	}

	// no-op if ReplicationReady is false
	isFalse := r.Vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)
	if isFalse {
		r.Log.Info("Stopped reconciling status: not ready")
		return ctrl.Result{}, nil
	}

	// no-op if there is no transaction ID we can use to query replication status
	if r.Vrep.Status.TransactionID == 0 {
		r.Log.Info("Stopped reconciling status: transaction ID 0")
		return ctrl.Result{}, nil
	}

	// fetch target VerticaDB
	if res, fetchErr := r.fetchTargetVdb(ctx); verrors.IsReconcileAborted(res, fetchErr) {
		return res, fetchErr
	}

	// determine usernames and passwords
	err := r.determineUsernameAndPassword(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// collect pod facts for target sandbox (or main cluster)
	err = r.collectPodFacts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// choose the source host and target host
	// (first host where db is up in the specified cluster)
	err = r.determineTargetHosts()
	if err != nil {
		return ctrl.Result{}, err
	}

	// build all the opts
	opts := r.buildOpts()

	// setup dispatcher for vclusterops API
	err = r.makeDispatcher()
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.runReplicationStatus(ctx, r.dispatcher, opts)

	return ctrl.Result{}, err
}

// fetch the source and target VerticaDBs
func (r *ReplicationStatusReconciler) fetchTargetVdb(ctx context.Context) (res ctrl.Result, err error) {
	vdb := &vapi.VerticaDB{}
	nm := names.GenNamespacedName(r.Vrep, r.Vrep.Spec.Target.VerticaDB)
	res, err = vk8s.FetchVDB(ctx, r.VRec, r.Vrep, nm, vdb)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	r.TargetInfo.Vdb = vdb
	return
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (r *ReplicationStatusReconciler) makeDispatcher() error {
	if vmeta.UseVClusterOps(r.TargetInfo.Vdb.Annotations) {
		r.dispatcher = vadmin.MakeVClusterOpsWithTarget(r.Log, r.TargetInfo.Vdb, r.TargetInfo.Vdb,
			r.VRec.GetClient(), r.TargetInfo.Password, r.VRec, vadmin.SetupVClusterOps)
		return nil
	}
	return fmt.Errorf("replication is not supported when the source uses admintools deployments")
}

// determine usernames and passwords for both source and target VerticaDBs
func (r *ReplicationStatusReconciler) determineUsernameAndPassword(ctx context.Context) (err error) {
	r.TargetInfo.Username, r.TargetInfo.Password, err = setUsernameAndPassword(ctx,
		r.Client, r.Log, r.VRec, r.TargetInfo.Vdb, &r.Vrep.Spec.Source.VerticaReplicatorDatabaseInfo)
	if err != nil {
		return err
	}

	return
}

// collect pod facts for source and target sandboxes (or main clusters)
func (r *ReplicationStatusReconciler) collectPodFacts(ctx context.Context) (err error) {
	r.TargetPFacts, err = r.makePodFacts(ctx, r.TargetInfo.Vdb,
		r.Vrep.Spec.Target.SandboxName)
	if err != nil {
		return
	}
	if err = r.TargetPFacts.Collect(ctx, r.TargetInfo.Vdb); err != nil {
		return
	}
	return
}

// choose the source host and target host
// (first host where db is up in the specified cluster)
func (r *ReplicationStatusReconciler) determineTargetHosts() (err error) {
	// assume target must not be read-only, no subcluster constraints
	upPodIP, ok := r.TargetPFacts.FindFirstUpPodIP(false, "")
	if !ok {
		err = fmt.Errorf("cannot find any up hosts in target database cluster")
		return
	} else {
		r.TargetInfo.IP = upPodIP
	}
	return
}

// make podfacts for a cluster (either main or a sandbox) of a vdb
func (r *ReplicationStatusReconciler) makePodFacts(ctx context.Context, vdb *vapi.VerticaDB,
	sandboxName string) (*podfacts.PodFacts, error) {
	username := vdb.GetVerticaUser()
	password, err := vk8s.GetSuperuserPassword(ctx, r.Client, r.Log, r.VRec, vdb)
	if err != nil {
		return nil, err
	}
	prunner := cmds.MakeClusterPodRunner(r.Log, r.VRec.Cfg, username, password)
	pFacts := podfacts.MakePodFactsForSandbox(r.VRec, prunner, r.Log, password, sandboxName)
	return &pFacts, nil
}

// build all the opts from the cached values in reconciler
func (r *ReplicationStatusReconciler) buildOpts() []replicationstatus.Option {
	opts := []replicationstatus.Option{
		replicationstatus.WithTargetIP(r.TargetInfo.IP),
		replicationstatus.WithTargetDBName(r.TargetInfo.Vdb.Spec.DBName),
		replicationstatus.WithTargetUserName(r.TargetInfo.Username),
		replicationstatus.WithTargetPassword(r.TargetInfo.Password),
		replicationstatus.WithTransactionID(r.Vrep.Status.TransactionID),
	}
	return opts
}

func (r *ReplicationStatusReconciler) runReplicationStatus(ctx context.Context, dispatcher vadmin.Dispatcher,
	opts []replicationstatus.Option) (err error) {
	// TODO: Turn these into annotations
	timeout := 60
	pollingFrequency := 0
	pollingDuration := time.Duration(pollingFrequency * int(time.Second))

	r.Log.Info(fmt.Sprintf("Starting polling for transaction ID %d", r.Vrep.Status.TransactionID))
	for i := 0; i < timeout; i += pollingFrequency {
		// call vcluster API
		status, errRun := dispatcher.GetReplicationStatus(ctx, opts...)
		if errRun != nil {
			return errRun
		}

		if status.Status == statusFailed {
			r.VRec.Event(r.Vrep, corev1.EventTypeWarning, events.ReplicationFailed, "Failed when calling replication start")

			// clear Replicating status condition and set the ReplicationComplete status condition
			err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
				[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, "Failed"),
					vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, "Failed")},
				stateFailedReplication, r.Vrep.Status.TransactionID)
			if err != nil {
				errRun = errors.Join(errRun, err)
			}
			return errRun
		}

		if status.OpName == opLoadSnapshot && status.Status == statusCompleted {
			// Parse start/end times for event message
			startTime, err := time.Parse(time.UnixDate, status.StartTime)
			if err != nil {
				return err
			}
			endTime, err := time.Parse(time.UnixDate, status.EndTime)
			if err != nil {
				return err
			}

			r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationSucceeded,
				"Successfully replicated database in %s", endTime.Sub(startTime).Truncate(time.Second))

			return vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
				[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, v1beta1.ReasonSucceeded),
					vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, v1beta1.ReasonSucceeded)},
				stateSucceededReplication, r.Vrep.Status.TransactionID)
		}

		time.Sleep(pollingDuration)
	}
	return fmt.Errorf("replication timeout exceeded")
}
