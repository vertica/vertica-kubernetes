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
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	vrepstatus "github.com/vertica/vertica-kubernetes/pkg/vrepstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	stateReplicating          = "Replicating"
	stateSucceededReplication = "Replication successful"
	stateFailedReplication    = "Replication failed"
)

type ReplicationInfo struct {
	Vdb      *vapi.VerticaDB
	IP       string
	Username string
	Password string
}

type ReplicationReconciler struct {
	client.Client
	VRec         *VerticaReplicatorReconciler
	Vrep         *v1beta1.VerticaReplicator
	dispatcher   vadmin.Dispatcher
	SourcePFacts *podfacts.PodFacts
	TargetPFacts *podfacts.PodFacts
	Log          logr.Logger
	SourceInfo   *ReplicationInfo
	TargetInfo   *ReplicationInfo
}

func MakeReplicationReconciler(cli client.Client, r *VerticaReplicatorReconciler, vrep *v1beta1.VerticaReplicator,
	log logr.Logger) controllers.ReconcileActor {
	return &ReplicationReconciler{
		Client:     cli,
		VRec:       r,
		Vrep:       vrep,
		Log:        log.WithName("ReplicationReconciler"),
		SourceInfo: &ReplicationInfo{},
		TargetInfo: &ReplicationInfo{},
	}
}

func (r *ReplicationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (res ctrl.Result, err error) {
	// no-op if ReplicationComplete is present (either true or false)
	isPresent := r.Vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)
	if isPresent {
		return ctrl.Result{}, nil
	}

	// no-op if Replicating is true (this is possible with async replication)
	isReplicating := r.Vrep.IsStatusConditionTrue(v1beta1.Replicating)
	if isReplicating && r.Vrep.IsUsingAsyncReplication() {
		return ctrl.Result{}, nil
	}

	// no-op if ReplicationReady is false
	isFalse := r.Vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)
	if isFalse {
		return ctrl.Result{}, nil
	}

	// fetch both source and target VerticaDBs
	if res, fetchErr := r.fetchVdbs(ctx); verrors.IsReconcileAborted(res, fetchErr) {
		return res, fetchErr
	}

	// determine usernames and passwords for both source and target VerticaDBs
	err = r.determineUsernameAndPassword(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// collect pod facts for source and target sandboxes (or main clusters)
	err = r.collectPodFacts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.checkSandboxExists()
	if err != nil {
		return ctrl.Result{}, err
	}

	// choose the source host and target host
	// (first host where db is up in the specified cluster)
	err = r.determineSourceAndTargetHosts()
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

	err = r.runReplicateDB(ctx, r.dispatcher, opts)

	return ctrl.Result{}, err
}

// fetch the source and target VerticaDBs
func (r *ReplicationReconciler) fetchVdbs(ctx context.Context) (res ctrl.Result, err error) {
	vdbSource, vdbTarget, res, err := fetchSourceAndTargetVDBs(ctx, r.VRec, r.Vrep)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	r.SourceInfo.Vdb = vdbSource
	r.TargetInfo.Vdb = vdbTarget

	return
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (r *ReplicationReconciler) makeDispatcher() error {
	if !vmeta.UseVClusterOps(r.SourceInfo.Vdb.Annotations) {
		return fmt.Errorf("replication is not supported when the source uses admintools deployments")
	}

	if r.Vrep.IsUsingAsyncReplication() {
		r.dispatcher = vadmin.MakeVClusterOpsWithTarget(r.Log, r.SourceInfo.Vdb, r.TargetInfo.Vdb,
			r.VRec.GetClient(), r.SourceInfo.Password, r.VRec, vadmin.SetupVClusterOps)
	} else {
		r.dispatcher = vadmin.MakeVClusterOps(r.Log, r.SourceInfo.Vdb,
			r.VRec.GetClient(), r.SourceInfo.Password, r.VRec, vadmin.SetupVClusterOps)
	}
	return nil
}

// determine usernames and passwords for both source and target VerticaDBs
func (r *ReplicationReconciler) determineUsernameAndPassword(ctx context.Context) (err error) {
	r.SourceInfo.Username, r.SourceInfo.Password, err = setUsernameAndPassword(ctx,
		r.Client, r.Log, r.VRec, r.SourceInfo.Vdb, &r.Vrep.Spec.Source.VerticaReplicatorDatabaseInfo)
	if err != nil {
		return err
	}

	r.TargetInfo.Username, r.TargetInfo.Password, err = setUsernameAndPassword(ctx,
		r.Client, r.Log, r.VRec, r.TargetInfo.Vdb, &r.Vrep.Spec.Target.VerticaReplicatorDatabaseInfo)
	if err != nil {
		return err
	}

	return
}

// determine username and password to use for a vdb depending on certain fields of vrep spec
func setUsernameAndPassword(ctx context.Context, cli client.Client, log logr.Logger,
	vRec *VerticaReplicatorReconciler, vdb *vapi.VerticaDB,
	dbInfo *v1beta1.VerticaReplicatorDatabaseInfo) (username, password string, err error) {
	if dbInfo.UserName == "" {
		// database superuser is assumed
		username := vdb.GetVerticaUser()
		password, err := vk8s.GetSuperuserPassword(ctx, cli, log, vRec, vdb)
		if err != nil {
			return "", "", err
		}
		return username, password, nil
	} else {
		// custom username and password is used
		username := dbInfo.UserName
		if dbInfo.PasswordSecret == "" {
			// empty password is assumed
			return username, "", nil
		} else {
			// fetch custom password
			// assuming the password secret key is default
			password, err := vk8s.GetCustomSuperuserPassword(ctx, cli, log,
				vRec, vdb, dbInfo.PasswordSecret, names.SuperuserPasswordKey)
			if err != nil {
				return "", "", err
			}
			return username, password, nil
		}
	}
}

// collect pod facts for source and target sandboxes (or main clusters)
func (r *ReplicationReconciler) collectPodFacts(ctx context.Context) (err error) {
	r.SourcePFacts, err = r.makePodFacts(ctx, r.SourceInfo.Vdb,
		r.Vrep.Spec.Source.SandboxName)
	if err != nil {
		return
	}
	if err = r.SourcePFacts.Collect(ctx, r.SourceInfo.Vdb); err != nil {
		return
	}

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

// return error if either source or destination sandbox doesn't exist or has no node assigned to it
func (r *ReplicationReconciler) checkSandboxExists() error {
	if len(r.SourcePFacts.Detail) == 0 && r.SourcePFacts.SandboxName != vapi.MainCluster {
		return fmt.Errorf("source sandbox '%s' does not exist or has no nodes assigned to it", r.SourcePFacts.SandboxName)
	}
	if len(r.TargetPFacts.Detail) == 0 && r.TargetPFacts.SandboxName != vapi.MainCluster {
		return fmt.Errorf("target sandbox '%s' does not exist or has no nodes assigned to it", r.TargetPFacts.SandboxName)
	}
	return nil
}

// choose the source host and target host
// (first host where db is up in the specified cluster)
func (r *ReplicationReconciler) determineSourceAndTargetHosts() (err error) {
	// assume source could be read-only, no subcluster constraints
	upPodIP, ok := r.SourcePFacts.FindFirstUpPodIP(true, "")
	if !ok {
		err = fmt.Errorf("cannot find any up hosts in source database cluster")
		return
	} else {
		r.SourceInfo.IP = upPodIP
	}
	// assume target must not be read-only, no subcluster constraints
	upPodIP, ok = r.TargetPFacts.FindFirstUpPodIP(false, "")
	if !ok {
		err = fmt.Errorf("cannot find any up hosts in target database cluster")
		return
	} else {
		r.TargetInfo.IP = upPodIP
	}
	return
}

// make podfacts for a cluster (either main or a sandbox) of a vdb
func (r *ReplicationReconciler) makePodFacts(ctx context.Context, vdb *vapi.VerticaDB,
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
func (r *ReplicationReconciler) buildOpts() []replicationstart.Option {
	opts := []replicationstart.Option{
		replicationstart.WithSourceIP(r.SourceInfo.IP),
		replicationstart.WithSourceUsername(r.SourceInfo.Username),
		replicationstart.WithTargetIP(r.TargetInfo.IP),
		replicationstart.WithTargetDBName(r.TargetInfo.Vdb.Spec.DBName),
		replicationstart.WithTargetUserName(r.TargetInfo.Username),
		replicationstart.WithTargetPassword(r.TargetInfo.Password),
		replicationstart.WithSourceTLSConfig(r.Vrep.Spec.TLSConfig),
		replicationstart.WithSourceSandboxName(r.Vrep.Spec.Source.SandboxName),
		replicationstart.WithAsync(r.Vrep.IsUsingAsyncReplication()),
		replicationstart.WithObjectName(r.Vrep.Spec.Source.ObjectName),
		replicationstart.WithIncludePattern(r.Vrep.Spec.Source.IncludePattern),
		replicationstart.WithExcludePattern(r.Vrep.Spec.Source.ExcludePattern),
		replicationstart.WithTargetNamespace(r.Vrep.Spec.Target.Namespace),
	}
	return opts
}

func (r *ReplicationReconciler) runReplicateDB(ctx context.Context, dispatcher vadmin.Dispatcher,
	opts []replicationstart.Option) (err error) {
	// set Replicating status condition and state prior to calling vclusterops API
	err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionTrue, "Started")}, stateReplicating, 0)
	if err != nil {
		return err
	}

	// call vcluster API
	r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationStarted,
		"Starting replication")
	start := time.Now()
	transactionID, errRun := dispatcher.ReplicateDB(ctx, opts...)
	if errRun != nil {
		r.VRec.Event(r.Vrep, corev1.EventTypeWarning, events.ReplicationFailed, "Failed when calling replication start")
		// clear Replicating status condition and set the ReplicationComplete status condition
		err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, "Failed"),
				vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, "Failed")}, stateFailedReplication, 0)
		if err != nil {
			errRun = errors.Join(errRun, err)
		}
		return errRun
	}

	if r.Vrep.IsUsingAsyncReplication() {
		// Asynchronous replication has just started when ReplicateDB returns
		r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationStarted,
			"Successfully started database replication in %s", time.Since(start).Truncate(time.Second))

		// Update Replicating status condition with the transaction ID
		return vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionTrue, "Started")}, stateReplicating, transactionID)
	} else {
		// Synchronous replication is complete when ReplicateDB returns
		r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationSucceeded,
			"Successfully replicated database in %s", time.Since(start).Truncate(time.Second))

		// clear Replicating status condition and set the ReplicationComplete status condition
		return vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, v1beta1.ReasonSucceeded),
				vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, v1beta1.ReasonSucceeded)}, stateSucceededReplication, 0)
	}
}
