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
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
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

type ReplicationReconciler struct {
	client.Client
	VRec           *VerticaReplicatorReconciler
	Vrep           *v1beta1.VerticaReplicator
	dispatcher     vadmin.Dispatcher
	SourcePFacts   *vdbcontroller.PodFacts
	TargetPFacts   *vdbcontroller.PodFacts
	Log            logr.Logger
	SourceVdb      *vapi.VerticaDB
	SourceIP       string
	TargetVdb      *vapi.VerticaDB
	TargetIP       string
	TargetDBName   string
	SourceUsername string
	SourcePassword string
	TargetUsername string
	TargetPassword string
}

func MakeReplicationReconciler(cli client.Client, r *VerticaReplicatorReconciler, vrep *v1beta1.VerticaReplicator,
	log logr.Logger) controllers.ReconcileActor {
	return &ReplicationReconciler{
		Client: cli,
		VRec:   r,
		Vrep:   vrep,
		Log:    log.WithName("ReplicationReconciler"),
	}
}

func (r *ReplicationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (res ctrl.Result, err error) {
	// no-op if ReplicationComplete is present (either true or false)
	isPresent := r.Vrep.IsStatusConditionPresent(v1beta1.ReplicationComplete)
	if isPresent {
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

	// determine db names
	r.determineDBNames()

	// determine usernames and passwords
	err = r.determineUsernameAndPassword(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// collect pod facts for source and target sandboxes (or main clusters)
	err = r.collectPodFacts(ctx)
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
	// fetch source vdb
	sourceVdb := &vapi.VerticaDB{}
	sourceName := names.GenNamespacedName(r.Vrep, r.Vrep.Spec.Source.VerticaDB)
	if res, err = vk8s.FetchVDB(ctx, r.VRec, r.Vrep, sourceName, sourceVdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.SourceVdb = sourceVdb
	// fetch target vdb
	targetVdb := &vapi.VerticaDB{}
	targetName := names.GenNamespacedName(r.Vrep, r.Vrep.Spec.Target.VerticaDB)
	if res, err = vk8s.FetchVDB(ctx, r.VRec, r.Vrep, targetName, targetVdb); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.TargetVdb = targetVdb

	return
}

// makeDispatcher will create a Dispatcher object based on the feature flags set.
func (r *ReplicationReconciler) makeDispatcher() error {
	if vmeta.UseVClusterOps(r.SourceVdb.Annotations) {
		r.dispatcher = vadmin.MakeVClusterOps(r.Log, r.SourceVdb,
			r.VRec.GetClient(), r.SourcePassword, r.VRec, vadmin.SetupVClusterOps)
		return nil
	}
	return fmt.Errorf("replication is not supported for source VerticaDB with admintools deployment")
}

func (r *ReplicationReconciler) determineDBNames() {
	r.TargetDBName = r.TargetVdb.Spec.DBName
}

func (r *ReplicationReconciler) determineUsernameAndPassword(ctx context.Context) (err error) {
	r.SourceUsername, r.SourcePassword, err = setUsernameAndPassword(ctx,
		r.Client, r.Log, r.VRec, r.SourceVdb, &r.Vrep.Spec.Source)
	if err != nil {
		return err
	}

	r.TargetUsername, r.TargetPassword, err = setUsernameAndPassword(ctx,
		r.Client, r.Log, r.VRec, r.TargetVdb, &r.Vrep.Spec.Target)
	if err != nil {
		return err
	}

	return
}

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
				vRec, vdb, dbInfo.PasswordSecret, "")
			if err != nil {
				return "", "", err
			}
			return username, password, nil
		}
	}
}

func (r *ReplicationReconciler) collectPodFacts(ctx context.Context) (err error) {
	r.SourcePFacts, err = r.makePodFacts(ctx, r.SourceVdb,
		r.Vrep.Spec.Source.SandboxName)
	if err != nil {
		return
	}
	if err = r.SourcePFacts.Collect(ctx, r.SourceVdb); err != nil {
		return
	}

	r.TargetPFacts, err = r.makePodFacts(ctx, r.TargetVdb,
		r.Vrep.Spec.Target.SandboxName)
	if err != nil {
		return
	}
	if err = r.TargetPFacts.Collect(ctx, r.TargetVdb); err != nil {
		return
	}
	return
}

func (r *ReplicationReconciler) determineSourceAndTargetHosts() (err error) {
	// assume source must not be read-only, no subcluster constraints
	upPodIP, ok := r.SourcePFacts.FindFirstUpPodIP(false, "")
	if !ok {
		err = fmt.Errorf("cannot find any up hosts in source database cluster")
		return
	} else {
		r.SourceIP = upPodIP
	}
	// assume target must not be read-only, no subcluster constraints
	upPodIP, ok = r.TargetPFacts.FindFirstUpPodIP(false, "")
	if !ok {
		err = fmt.Errorf("cannot find any up hosts in target database cluster")
		return
	} else {
		r.TargetIP = upPodIP
	}
	return
}

func (r *ReplicationReconciler) makePodFacts(ctx context.Context, vdb *vapi.VerticaDB,
	sandboxName string) (*vdbcontroller.PodFacts, error) {
	username := vdb.GetVerticaUser()
	password, err := vk8s.GetSuperuserPassword(ctx, r.Client, r.Log, r.VRec, vdb)
	if err != nil {
		return nil, err
	}
	prunner := cmds.MakeClusterPodRunner(r.Log, r.VRec.Cfg, username, password)
	pFacts := vdbcontroller.MakePodFactsForSandbox(r.VRec, prunner, r.Log, password, sandboxName)
	return &pFacts, nil
}

func (r *ReplicationReconciler) buildOpts() []replicationstart.Option {
	opts := []replicationstart.Option{
		replicationstart.WithSourceIP(r.SourceIP),
		replicationstart.WithSourceUsername(r.SourceUsername),
		replicationstart.WithTargetIP(r.TargetIP),
		replicationstart.WithTargetDBName(r.TargetDBName),
		replicationstart.WithTargetUserName(r.TargetUsername),
		replicationstart.WithTargetPassword(r.TargetPassword),
		replicationstart.WithSourceTLSConfig(r.Vrep.Spec.TLSConfig),
	}
	return opts
}

func (r *ReplicationReconciler) runReplicateDB(ctx context.Context, dispatcher vadmin.Dispatcher,
	opts []replicationstart.Option) (err error) {
	// set Replicating status condition and state prior to calling vclusterops API
	err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionTrue, "Started")}, stateReplicating)
	if err != nil {
		return err
	}

	// call showRestorePoints vcluster API
	r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationStarted,
		"Starting replication")
	start := time.Now()
	_, errRun := dispatcher.ReplicateDB(ctx, opts...)
	if errRun != nil {
		r.VRec.Event(r.Vrep, corev1.EventTypeWarning, events.ReplicationFailed, "Failed when calling replication start")
		// clear Replicating status condition and set the ReplicationComplete status condition
		err = vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, "Failed"),
				vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, "Failed")}, stateFailedReplication)
		if err != nil {
			errRun = errors.Join(errRun, err)
		}
		return errRun
	}
	r.VRec.Eventf(r.Vrep, corev1.EventTypeNormal, events.ReplicationSucceeded,
		"Successfully replicated database in %s", time.Since(start).Truncate(time.Second))

	// clear Replicating status condition and set the ReplicationComplete status condition
	return vrepstatus.Update(ctx, r.VRec.Client, r.VRec.Log, r.Vrep,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Replicating, metav1.ConditionFalse, "Succeeded"),
			vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, "Succeeded")}, stateSucceededReplication)
}
