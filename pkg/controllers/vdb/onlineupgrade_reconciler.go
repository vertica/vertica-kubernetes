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

package vdb

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/catalog"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/manageconnectiondraining"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/renamesc"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// When we generate a sandbox for the upgrade, this is preferred name of that sandbox.
const preferredSandboxName = "replica-group-b"

const (
	ConfigParamLevelDatabase                 = ""
	ConfigParamBoolTrue                      = "1"
	ConfigParamBoolFalse                     = "0"
	ConfigParamDisableNonReplicatableQueries = "DisableNonReplicatableQueries"
	RedirectConnectionTimeoutSeconds         = 5 * 60
)

const (
	archiveBeforeRepBaseName = "archive_before_rep"
)

// List of status messages for online upgrade. When adding a new entry here,
// be sure to add a *StatusMsgInx const below.
var onlineUpgradeStatusMsgs = []string{
	"Starting online upgrade",
	"Create new subclusters to mimic subclusters in the main cluster",
	fmt.Sprintf("Querying the original value of config parameter %q", ConfigParamDisableNonReplicatableQueries),
	fmt.Sprintf("Disable non-replicatable queries by setting config parameter %q", ConfigParamDisableNonReplicatableQueries),
	"Sandbox subclusters",
	fmt.Sprintf("clear config parameter %q on sandbox", ConfigParamDisableNonReplicatableQueries),
	"Promote secondaries whose base subcluster is primary",
	"Upgrade sandbox to new version",
	"Pause connections to main cluster",
	"Preparing replication",
	"Back up database before replication",
	"Replicate new data from main cluster to sandbox",
	"Redirect connections to sandbox",
	"Promote sandbox to main cluster",
	"Remove original main cluster",
	"Rename subclusters in new main cluster",
}

// Constants for each entry in onlineUpgradeStatusMsgs
const (
	startOnlineUpgradeStatusMsgInx = iota
	createNewSubclustersStatusMsgInx
	queryOriginalConfigParamDisableNonReplicatableQueriesMsgInx
	disableNonReplicatableQueriesMsgInx
	sandboxSubclustersMsgInx
	clearDisableNonReplicatableQueriesMsgInx
	promoteSubclustersInSandboxMsgInx
	upgradeSandboxMsgInx
	pauseConnectionsMsgInx
	prepareReplicationInx
	backupDBBeforeReplicationMsgInx
	startReplicationMsgInx
	redirectToSandboxMsgInx
	promoteSandboxMsgInx
	removeOriginalClusterMsgInx
	renameScsInMainClusterMsgInx
)

// Constants for some steps during online upgrade
const (
	runObjRecForMainInx = iota
	addSubclustersInx
	addNodeInx
	rebalanceShardsInx
	setConfigParamInx
	sandboxInx
	clearConfigParamInx
	waitForSandboxUpgradeInx
	waitForConnectionsPauseInx
	backupBeforeReplicationInx
	replicationInx
	promoteSandboxInx
	removeReplicaGroupAInx
	deleteReplicaGroupAStsInx
)

// OnlineUpgradeReconciler will coordinate an online upgrade that allows
// write. This is done by splitting the cluster into two separate replicas and
// using failover strategies to keep the database online.
// We have podfacts for main cluster and replica sandbox.
type OnlineUpgradeReconciler struct {
	VRec                                                  *VerticaDBReconciler
	Log                                                   logr.Logger
	VDB                                                   *vapi.VerticaDB
	PFacts                                                map[string]*podfacts.PodFacts
	Manager                                               UpgradeManager
	Dispatcher                                            vadmin.Dispatcher
	sandboxName                                           string // name of the sandbox created for replica group B
	originalConfigParamDisableNonReplicatableQueriesValue string
}

// MakeOnlineUpgradeReconciler will build a OnlineUpgradeReconciler object
func MakeOnlineUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &OnlineUpgradeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("OnlineUpgradeReconciler"),
		VDB:        vdb,
		PFacts:     map[string]*podfacts.PodFacts{vapi.MainCluster: pfacts},
		Manager:    *MakeUpgradeManager(vdbrecon, log, vdb, vapi.OnlineUpgradeInProgress, onlineUpgradeAllowed),
		Dispatcher: dispatcher,
	}
}

// Reconcile will automate the process of a online upgrade.
//
//nolint:funlen
func (r *OnlineUpgradeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if ok, err := r.Manager.IsUpgradeNeeded(ctx, vapi.MainCluster); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := r.PFacts[vapi.MainCluster].Collect(ctx, r.VDB); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.Manager.logUpgradeStarted(vapi.MainCluster); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an upgrade by setting condition and event recording
		r.startUpgrade,
		r.logEventIfThisUpgradeWasNotChosen,
		r.postStartOnlineUpgradeMsg,
		// Load up state that is used for the subsequent steps
		r.loadUpgradeState,
		// Assign subclusters to upgrade to replica group A
		r.assignSubclustersToReplicaGroupA,
		// Create secondary subclusters for each of the subclusters. These will be
		// added to replica group B and ready to be sandboxed.
		r.postCreateNewSubclustersMsg,
		r.assignSubclustersToReplicaGroupB,
		r.runObjReconcilerForMainCluster,
		r.runAddSubclusterReconcilerForMainCluster,
		r.runAddNodesReconcilerForMainCluster,
		r.runRebalanceSandboxSubcluster,
		// Get the original value of config parameter DisableNonReplicatableQueries at database level
		r.postQueryOriginalConfigParamDisableNonReplicatableQueriesMsg,
		r.queryOriginalConfigParamDisableNonReplicatableQueries,
		// Disable all non-replicatable queries by setting config parameter DisableNonReplicatableQueries
		// at database level
		r.postDisableNonReplicatableQueriesMsg,
		r.setConfigParamDisableNonReplicatableQueries,
		// Sandbox all of the secondary subclusters that are destined for
		// replica group B.
		r.postSandboxSubclustersMsg,
		r.sandboxReplicaGroupB,
		// workaround: clear the value to force vertica.conf to be rewritten
		r.postClearConfigParamDisableNonReplicatableQueriesMsg,
		r.clearConfigParamDisableNonReplicatableQueries,
		// Change replica b subcluster types to match the main cluster's
		r.postPromoteSubclustersInSandboxMsg,
		r.promoteReplicaBSubclusters,
		// Upgrade the version in the sandbox to the new version.
		r.postUpgradeSandboxMsg,
		r.upgradeSandbox,
		r.waitForSandboxUpgrade,
		// Prepare replication by ensuring nodes are up
		r.postPrepareReplicationMsg,
		r.prepareReplication,
		// Pause all connections to replica A. This is to prepare for the
		// replication below.
		r.postPauseConnectionsMsg,
		r.pauseConnectionsAtReplicaGroupA,
		r.waitForConnectionsPaused,
		// Back up database before replication
		r.postBackupDBBeforeReplicationMsg,
		r.createRestorePointBeforeReplication,
		// Copy any new data that was added since the sandbox from replica group
		// A to replica group B.
		r.postStartReplicationMsg,
		r.startReplicationToReplicaGroupB,
		r.waitForReplicateToReplicaGroupB,
		// replicate v_internal_tables.v_redirect_state to sandbox
		r.copyRedirectStateToReplicaGroupB,
		// Redirect all of the connections to replica group A to replica group B.
		r.postRedirectToSandboxMsg,
		r.redirectConnectionsToReplicaGroupB,
		// Promote the sandbox to the main cluster and discard the pods for the
		// old main.
		r.postPromoteSandboxMsg,
		r.promoteSandboxToMainCluster,
		r.deleteSandboxConfigMap,
		// wait for all clients to be redirected to the new main
		r.waitForConnectionRedirect,
		// Remove original main cluster. We will remove replica group A since
		// replica group B is promoted to main cluster now.
		r.postRemoveOriginalClusterMsg,
		r.removeReplicaGroupAFromVdb,
		r.removeReplicaGroupA,
		r.deleteReplicaGroupASts,
		// Rename subclusters in new main cluster to match the original main cluster.
		r.postRenameScsInMainClusterMsg,
		r.renameReplicaGroupBFromVdb,
		// Cleanup up the condition and event recording for a completed upgrade
		r.finishUpgrade,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); verrors.IsReconcileAborted(res, err) {
			// If Reconcile was aborted with a requeue, set the RequeueAfter interval to prevent exponential backoff
			if err == nil {
				res.Requeue = false
				res.RequeueAfter = r.VDB.GetUpgradeRequeueTimeDuration()
			}
			return res, err
		}
	}

	return ctrl.Result{}, r.Manager.logUpgradeSucceeded(vapi.MainCluster)
}

func (r *OnlineUpgradeReconciler) startUpgrade(ctx context.Context) (ctrl.Result, error) {
	return r.Manager.startUpgrade(ctx, vapi.MainCluster)
}

// logEventIfThisUpgradeWasNotChosen will write an event log if we are doing this
// upgrade method as a fall back from a requested policy.
func (r *OnlineUpgradeReconciler) logEventIfThisUpgradeWasNotChosen(_ context.Context) (ctrl.Result, error) {
	r.Manager.logEventIfRequestedUpgradeIsDifferent(vapi.OnlineUpgrade)
	return ctrl.Result{}, nil
}

func (r *OnlineUpgradeReconciler) finishUpgrade(ctx context.Context) (ctrl.Result, error) {
	return r.Manager.finishUpgrade(ctx, vapi.MainCluster)
}

// postStartOnlineUpgradeMsg will update the status message to indicate that
// we are starting online upgrade.
func (r *OnlineUpgradeReconciler) postStartOnlineUpgradeMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, startOnlineUpgradeStatusMsgInx)
}

// loadUpgradeState will load state into the reconciler that
// is used in subsequent steps.
func (r *OnlineUpgradeReconciler) loadUpgradeState(ctx context.Context) (ctrl.Result, error) {
	err := r.Manager.cachePrimaryImages(ctx, vapi.MainCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.sandboxName = vmeta.GetOnlineUpgradeSandbox(r.VDB.Annotations)
	r.Log.Info("load upgrade state", "sandboxName", r.sandboxName, "primaryImages", r.Manager.PrimaryImages)
	return ctrl.Result{}, nil
}

// assignSubclustersToReplicaGroupA will go through all of the subclusters involved
// in the upgrade and assign them to the first replica group. The assignment is
// saved in the status.upgradeState.replicaGroups field.
func (r *OnlineUpgradeReconciler) assignSubclustersToReplicaGroupA(ctx context.Context) (ctrl.Result, error) {
	// Early out if we have already promoted and removed replica group A, or we have already created replica group A.
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > removeReplicaGroupAInx ||
		r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) != 0 {
		return ctrl.Result{}, nil
	}

	// We simply assign all subclusters to the first group. This is used by
	// webhooks to prevent new subclusters being added that aren't part of the
	// upgrade.
	_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, r.assignSubclustersToReplicaGroupACallback)
	return ctrl.Result{}, err
}

// runObjReconcilerForMainCluster will run the object reconciler for all objects
// that are part of the main cluster. This is used to build or update any
// necessary objects the upgrade depends on.
func (r *OnlineUpgradeReconciler) runObjReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	// This obj reconciler must be done only once unless it fails
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > runObjRecForMainInx {
		return ctrl.Result{}, nil
	}

	rec := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// runAddSubclusterReconcilerForMainCluster will run the reconciler to create any necessary subclusters
func (r *OnlineUpgradeReconciler) runAddSubclusterReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	// We skip this if we have already added the new subclusters
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > addSubclustersInx {
		return ctrl.Result{}, nil
	}

	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddSubclusterReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// runAddNodesReconcilerForMainCluster will run the reconciler to scale out any subclusters.
func (r *OnlineUpgradeReconciler) runAddNodesReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	// We skip this if we have already added the new nodes
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > addNodeInx {
		return ctrl.Result{}, nil
	}

	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddNodeReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// runRebalanceSandboxSubcluster will run a rebalance against the subclusters that will be sandboxed.
func (r *OnlineUpgradeReconciler) runRebalanceSandboxSubcluster(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to touch old main cluster
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > rebalanceShardsInx {
		return ctrl.Result{}, nil
	}

	pf := r.PFacts[vapi.MainCluster]
	actor := MakeRebalanceShardsReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, "" /*all subclusters*/)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// postCreateNewSubclustersMsg will update the status message to indicate that
// we are about to create new subclusters to mimic the main cluster's subclusters.
func (r *OnlineUpgradeReconciler) postCreateNewSubclustersMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, createNewSubclustersStatusMsgInx)
}

// assignSubclustersToReplicaGroupB will figure out the subclusters that make up
// replica group B. We will add a secondary for each of the subcluster that
// exists in the main cluster. This is a pre-step to setting up replica group B, which will
// eventually exist in its own sandbox.
func (r *OnlineUpgradeReconciler) assignSubclustersToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to do this step
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > promoteSandboxInx {
		return ctrl.Result{}, nil
	}

	// Early out if subclusters have already been assigned to replica group B.
	if r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue) != 0 {
		return ctrl.Result{}, nil
	}

	updated, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, r.addNewSubclusters)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed trying to update VDB with new subclusters: %w", err)
	}
	if updated {
		r.Log.Info("new secondary subclusters added to mimic the existing subclusters", "len(subclusters)", len(r.VDB.Spec.Subclusters))
	}
	return ctrl.Result{}, nil
}

// postQueryOriginalConfigParamDisableNonReplicatableQueriesMsg updates the status message to indicate that
// we are going to query the original value of config parameter DisableNonReplicatableQueries.
func (r *OnlineUpgradeReconciler) postQueryOriginalConfigParamDisableNonReplicatableQueriesMsg(
	ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, queryOriginalConfigParamDisableNonReplicatableQueriesMsgInx)
}

// queryOriginalConfigParamDisableNonReplicatableQueries gets value of the config parameter
// DisableNonReplicatableQueries at database level within main cluster
func (r *OnlineUpgradeReconciler) queryOriginalConfigParamDisableNonReplicatableQueries(ctx context.Context) (res ctrl.Result, err error) {
	if r.originalConfigParamDisableNonReplicatableQueriesValue != "" ||
		vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > setConfigParamInx {
		return ctrl.Result{}, err
	}
	pf := r.PFacts[vapi.MainCluster]
	initiator, ok := pf.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		r.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	vc := catalog.MakeVCluster(r.VDB, pf.VerticaSUPassword, initiator.GetPodIP(), r.Log, r.VRec.Client, r.VRec.EVRec)
	r.originalConfigParamDisableNonReplicatableQueriesValue, err = vc.GetConfigurationParameter(ConfigParamDisableNonReplicatableQueries,
		ConfigParamLevelDatabase, vapi.MainCluster, ctx)
	return ctrl.Result{}, err
}

// postDisableNonReplicatableQueriesMsg updates the status message to indicate that
// we are going to disable non-replicatable queries by setting config parameter DisableNonReplicatableQueries.
func (r *OnlineUpgradeReconciler) postDisableNonReplicatableQueriesMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, disableNonReplicatableQueriesMsgInx)
}

// setConfigParamDisableNonReplicatableQueries sets the config parameter
// DisableNonReplicatableQueries to true ("1") at database level within a given cluster
func (r *OnlineUpgradeReconciler) setConfigParamDisableNonReplicatableQueries(ctx context.Context) (ctrl.Result, error) {
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > setConfigParamInx {
		return ctrl.Result{}, nil
	}
	if r.originalConfigParamDisableNonReplicatableQueriesValue == "1" {
		return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
	}
	res, err := r.setConfigParamDisableNonReplicatableQueriesImpl(ctx, ConfigParamBoolTrue, r.sandboxName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.Log.Info("set DisableNonReplicatableQueries in main cluster before sandboxing")
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// postClearConfigParamDisableNonReplicatableQueriesMsg updates the status message to indicate that
// we are going to clear the config parameter DisableNonReplicatableQueries.
func (r *OnlineUpgradeReconciler) postClearConfigParamDisableNonReplicatableQueriesMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, clearDisableNonReplicatableQueriesMsgInx)
}

// clearConfigParamDisableNonReplicatableQueries clears the config parameter
// DisableNonReplicatableQueries from the sandbox
func (r *OnlineUpgradeReconciler) clearConfigParamDisableNonReplicatableQueries(ctx context.Context) (ctrl.Result, error) {
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > clearConfigParamInx {
		return ctrl.Result{}, nil
	}
	// update podfacts for sandbox
	if _, err := r.getSandboxPodFacts(ctx, true); err != nil {
		return ctrl.Result{}, err
	}
	res, err := r.setConfigParamDisableNonReplicatableQueriesImpl(ctx, ConfigParamBoolFalse, r.sandboxName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.Log.Info(fmt.Sprintf("cleared DisableNonReplicatableQueries in sandbox %s", r.sandboxName))
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// setConfigParamDisableNonReplicatableQueriesImpl sets the config parameter
// DisableNonReplicatableQueries to a certain value at database level within a given cluster
func (r *OnlineUpgradeReconciler) setConfigParamDisableNonReplicatableQueriesImpl(ctx context.Context,
	value, clusterName string) (ctrl.Result, error) {
	pf := r.PFacts[clusterName]
	initiator, ok := pf.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		r.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	vc := catalog.MakeVCluster(r.VDB, pf.VerticaSUPassword, initiator.GetPodIP(), r.Log, r.VRec.Client, r.VRec.EVRec)
	err := vc.SetConfigurationParameter(ConfigParamDisableNonReplicatableQueries, value, ConfigParamLevelDatabase, clusterName, ctx)
	return ctrl.Result{}, err
}

// postSandboxSubclustersMsg will update the status message to indicate that
// we are going to sandbox subclusters for replica group b.
func (r *OnlineUpgradeReconciler) postSandboxSubclustersMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, sandboxSubclustersMsgInx)
}

// sandboxReplicaGroupB will move all of the subclusters in replica B to a new sandbox
func (r *OnlineUpgradeReconciler) sandboxReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Skip this step if sandboxing is already done
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > sandboxInx {
		return ctrl.Result{}, nil
	}

	r.Log.Info("Start sandbox of replica group B", "sandboxName", r.sandboxName)

	// If the sandbox is not yet created, update the VDB. We can skip this if we
	// are simply waiting for the sandbox to complete.
	if r.sandboxName == "" {
		_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, r.moveReplicaGroupBSubclusterToSandbox)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed trying to update VDB for sandboxing: %w", err)
		}
		r.sandboxName = vmeta.GetOnlineUpgradeSandbox(r.VDB.Annotations)
		if r.sandboxName == "" {
			return ctrl.Result{}, errors.New("could not find sandbox name in annotations")
		}
		r.Log.Info("Created new sandbox in vdb", "sandboxName", r.sandboxName)
	}

	sb := r.VDB.GetSandboxStatus(r.sandboxName)
	rgbSize := r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	// check if subclusters are already in the sandbox
	if !(sb != nil && rgbSize == len(sb.Subclusters)) {
		// The nodes in the subcluster to sandbox must be running in order for
		// sandboxing to work. For this reason, we need to use the restart
		// reconciler to restart any down nodes.
		res, err := r.restartMainCluster(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	if len(r.VDB.Spec.Sandboxes) == 0 {
		r.Log.Info("Still waiting for sandbox to be added in VDB")
		return ctrl.Result{Requeue: true}, nil
	}

	// Drive the actual sandbox command. When this returns we know the sandbox is complete.
	actor := MakeSandboxSubclusterReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], r.Dispatcher, r.VRec.Client, true)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	r.Log.Info("subclusters in replica group B have been sandboxed", "sandboxName", r.sandboxName)
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// postPromoteSubclustersInSandboxMsg will update the status message to indicate that
// we are going to prmote subclusters in sandbox.
func (r *OnlineUpgradeReconciler) postPromoteSubclustersInSandboxMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, promoteSubclustersInSandboxMsgInx)
}

// promoteReplicaBSubclusters promotes all of the secondaries in replica group B whose
// parent subcluster is primary
func (r *OnlineUpgradeReconciler) promoteReplicaBSubclusters(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to promote subclusters in sandbox
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > promoteSandboxInx {
		return ctrl.Result{}, nil
	}

	// Get the sandbox podfacts only to invalidate the cache
	sbPFacts, err := r.getSandboxPodFacts(ctx, false)
	if err != nil {
		return ctrl.Result{}, err
	}
	sbPFacts.Invalidate()
	actor := MakeAlterSubclusterTypeReconciler(r.VRec, r.Log, r.VDB, sbPFacts, r.Dispatcher)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// postUpgradeSandboxMsg will update the status message to indicate that
// we are going to upgrade the vertica version in the sandbox.
func (r *OnlineUpgradeReconciler) postUpgradeSandboxMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, upgradeSandboxMsgInx)
}

// upgradeSandbox will upgrade the nodes in replica group B (sandbox) to the new version.
func (r *OnlineUpgradeReconciler) upgradeSandbox(ctx context.Context) (ctrl.Result, error) {
	// We skip this if sandbox upgrade is already done
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > waitForSandboxUpgradeInx {
		return ctrl.Result{}, nil
	}

	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}

	// We can skip updating vdb if the image in the sandbox matches the image in the vdb.
	updated := false
	if sb.Image != r.VDB.Spec.Image {
		var err error
		updated, err = vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, r.setImageInSandbox)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("failed trying to update image in sandbox: %w", err)
		}
		if updated {
			r.Log.Info("update image in sandbox", "image", r.VDB.Spec.Image)

			// Get the sandbox podfacts only to invalidate the cache
			sbPFacts, err := r.getSandboxPodFacts(ctx, false)
			if err != nil {
				return ctrl.Result{}, err
			}
			sbPFacts.Invalidate()
		}
	}

	if updated {
		// wait for vdb to be updated
		sb = r.VDB.GetSandbox(r.sandboxName)
		if sb == nil {
			return ctrl.Result{}, fmt.Errorf("could not find sandbox %q", r.sandboxName)
		}
		if sb.Image != r.VDB.Spec.Image {
			r.Log.Info("Still waiting for sandbox image to be updated in VDB")
			return ctrl.Result{Requeue: true}, nil
		}
		// update sandbox config map
		act := MakeSandboxUpgradeReconciler(r.VRec, r.Log, r.VDB)
		r.Manager.traceActorReconcile(act)
		res, err := act.Reconcile(ctx, &ctrl.Request{})
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		r.Log.Info("sandbox config map has updated for an upgrade", "sandboxName", r.sandboxName)
	}
	return ctrl.Result{}, nil
}

// waitForSandboxUpgrade will wait for the sandbox upgrade to finish. It will
// continually check if the pods in the sandbox are up.
func (r *OnlineUpgradeReconciler) waitForSandboxUpgrade(ctx context.Context) (ctrl.Result, error) {
	// We skip this if sandbox upgrade already happened
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > waitForSandboxUpgradeInx {
		return ctrl.Result{}, nil
	}

	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("collected sandbox facts", "numPods", len(sbPFacts.Detail))
	for _, pf := range sbPFacts.Detail {
		r.Log.Info("sandbox pod fact", "pod", pf.GetName().Name, "image", pf.GetImage(), "up", pf.GetUpNode())
		if pf.GetImage() != r.VDB.Spec.Image || !pf.GetUpNode() {
			r.Log.Info("Still waiting for sandbox to be upgraded")
			return ctrl.Result{Requeue: true}, nil
		}
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// postPauseConnectionsMsg will update the status message to indicate that
// client connections are being paused at the main cluster.
func (r *OnlineUpgradeReconciler) postPauseConnectionsMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, pauseConnectionsMsgInx)
}

// pauseConnectionsAtReplicaGroupA will pause all connections to replica A. This
// is to prepare for the replication at the next step. We need to stop writes
// (momentarily) so that the two replica groups have the same data.
func (r *OnlineUpgradeReconciler) pauseConnectionsAtReplicaGroupA(ctx context.Context) (ctrl.Result, error) {
	// If we have already paused connections we don't need to pause connections
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > waitForConnectionsPauseInx {
		return ctrl.Result{}, nil
	}

	pf := r.PFacts[vapi.MainCluster]
	initiator, ok := pf.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		r.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	err := r.Dispatcher.ManageConnectionDraining(ctx,
		manageconnectiondraining.WithInitiator(initiator.GetPodIP()),
		manageconnectiondraining.WithAction(vclusterops.ActionPause),
	)

	return ctrl.Result{}, err
}

func (r *OnlineUpgradeReconciler) waitForConnectionsPaused(ctx context.Context) (ctrl.Result, error) {
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > waitForConnectionsPauseInx {
		return ctrl.Result{}, nil
	}

	pfacts := r.PFacts[vapi.MainCluster]
	_, ok := pfacts.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		r.Log.Info("No Up nodes found; Requeue reconciliation")
		return ctrl.Result{Requeue: true}, nil
	}

	// wait for all connections to pause
	timeout := vmeta.GetOnlineUpgradeTimeout(r.VDB.Annotations)
	for i := 0; i < timeout; i++ {
		if res, err := r.Manager.areAllConnectionsPaused(ctx, pfacts); err != nil {
			return ctrl.Result{}, err
		} else if !res {
			return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
		}
		time.Sleep(1 * time.Second)
	}

	// we hit the timeout so at least one session is unpaused. kill any unpaused sessions before continuing
	if err := r.Manager.closeAllUnpausedSessions(ctx, r.PFacts[vapi.MainCluster]); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// postPrepareReplicationMsg will update the status message to indicate that
// we are doing some preparation work before replication
func (r *OnlineUpgradeReconciler) postPrepareReplicationMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, prepareReplicationInx)
}

// prepareReplication makes sure there is at least an Up node in the main cluster
// and the sandbox, to perform replication.
// Once we start using services for replication, we will check only the scs served by the services.
func (r *OnlineUpgradeReconciler) prepareReplication(ctx context.Context) (ctrl.Result, error) {
	// Skip if the replication has already completed successfully or VerticaReplicator
	// already exists
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > replicationInx ||
		vmeta.GetOnlineUpgradeReplicator(r.VDB.Annotations) != "" {
		return ctrl.Result{}, nil
	}
	// we need fresh podfacts just in case some pods went down while
	// sandbox upgrade was in progress.
	r.PFacts[vapi.MainCluster].Invalidate()
	err := r.PFacts[vapi.MainCluster].Collect(ctx, r.VDB)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to collect podfacts for main cluster: %w", err)
	}
	_, ok := r.PFacts[vapi.MainCluster].FindFirstUpPodIP(true, "")
	if !ok {
		r.Log.Info("Cannot find any up hosts in main cluster. Restarting main cluster.")
		var res ctrl.Result
		res, err = r.restartMainCluster(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		return ctrl.Result{}, err
	}
	_, ok = sbPFacts.FindFirstUpPodIP(false, "")
	if !ok {
		r.Log.Info("Cannot find any up hosts in sandbox. Requeue.", "sandboxName", r.sandboxName)
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// postBackupDBBeforeReplicationMsg will update the status message to indicate that
// we backing up the db before replication.
func (r *OnlineUpgradeReconciler) postBackupDBBeforeReplicationMsg(ctx context.Context) (ctrl.Result, error) {
	if vmeta.GetSaveRestorePoint(r.VDB.Annotations) {
		return r.postNextStatusMsg(ctx, backupDBBeforeReplicationMsgInx)
	}
	return ctrl.Result{}, nil
}

// createRestorePointBeforeReplication will backup the db just before replication
func (r *OnlineUpgradeReconciler) createRestorePointBeforeReplication(ctx context.Context) (ctrl.Result, error) {
	// Skip if the db has already been backed up
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > backupBeforeReplicationInx {
		return ctrl.Result{}, nil
	}
	// We skip save restore point if the feature flag is not set
	// to true
	if !vmeta.GetSaveRestorePoint(r.VDB.Annotations) {
		return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
	}
	archive := genBaseNameWithUUID(archiveBeforeRepBaseName, "_")
	if testArchive := vmeta.GetOnlineUpgradeArchiveBeforeReplication(r.VDB.Annotations); testArchive != "" {
		archive = testArchive
	}
	return r.createRestorePoint(ctx, r.PFacts[vapi.MainCluster], archive)
}

// postStartReplicationMsg will update the status message to indicate that
// replication from the main to the sandbox is starting.
func (r *OnlineUpgradeReconciler) postStartReplicationMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, startReplicationMsgInx)
}

// startReplicationToReplicaGroupB will copy any new data that was added since
// the sandbox from replica group A to replica group B.
func (r *OnlineUpgradeReconciler) startReplicationToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Skip if the replication has already completed successfully
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > replicationInx {
		return ctrl.Result{}, nil
	}

	// Skip if the VerticaReplicator has already been created.
	if vmeta.GetOnlineUpgradeReplicator(r.VDB.Annotations) != "" {
		return ctrl.Result{}, nil
	}

	vrep := &v1beta1.VerticaReplicator{
		TypeMeta: metav1.TypeMeta{
			APIVersion: v1beta1.GroupVersion.String(),
			Kind:       v1beta1.VerticaReplicatorKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			GenerateName:    fmt.Sprintf("%s-", r.VDB.Name),
			Namespace:       r.VDB.Namespace,
			OwnerReferences: []metav1.OwnerReference{r.VDB.GenerateOwnerReference()},
		},
		Spec: v1beta1.VerticaReplicatorSpec{
			Source: v1beta1.VerticaReplicatorDatabaseInfo{
				VerticaDB: r.VDB.Name,
			},
			Target: v1beta1.VerticaReplicatorDatabaseInfo{
				VerticaDB:   r.VDB.Name,
				SandboxName: r.sandboxName,
			},
		},
	}
	err := r.VRec.Client.Create(ctx, vrep)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create the VerticaReplicator %q: %w", vrep.GenerateName, err)
	}
	r.Log.Info("VerticaReplicator created", "name", vrep.Name, "uuid", vrep.UID)

	// Update the vdb with the name of the replicator that was created.
	annotationUpdate := func() (bool, error) {
		if r.VDB.Annotations == nil {
			r.VDB.Annotations = make(map[string]string, 1)
		}
		r.VDB.Annotations[vmeta.OnlineUpgradeReplicatorAnnotation] = vrep.Name
		return true, nil
	}
	_, err = vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, annotationUpdate)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add replicator annotation to vdb: %w", err)
	}

	return ctrl.Result{}, nil
}

// waitForReplicateToReplicaGroupB will poll the VerticaReplicator waiting for the replication to finish.
func (r *OnlineUpgradeReconciler) waitForReplicateToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Skip if the replication has already completed successfully
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > replicationInx {
		return ctrl.Result{}, nil
	}

	vrepName := vmeta.GetOnlineUpgradeReplicator(r.VDB.Annotations)
	if vrepName == "" {
		return ctrl.Result{}, errors.New("Could not find the VerticaReplicator name in vdb annotations")
	}

	vrep := v1beta1.VerticaReplicator{}
	nm := types.NamespacedName{
		Name:      vrepName,
		Namespace: r.VDB.Namespace,
	}
	err := r.VRec.Client.Get(ctx, nm, &vrep)
	if err != nil {
		if kerrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("VerticaReplicator %q is not found", vrepName)
		}
		return ctrl.Result{}, fmt.Errorf("failed trying to fetch VerticaReplicator: %w", err)
	}

	cond := vrep.FindStatusCondition(v1beta1.ReplicationComplete)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		r.Log.Info("Requeue replication is not finished", "vrepName", vrepName)
		return ctrl.Result{Requeue: true}, nil
	}

	// Delete the VerticaReplicator. If replication was successful, we leave the annotation present in the
	// VerticaDB so that we skip these steps until the upgrade is finished.
	err = r.VRec.Client.Delete(ctx, &vrep)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete the VerticaReplicator %s: %w", vrepName, err)
	}
	succeeded := cond.Reason == v1beta1.ReasonSucceeded
	if !succeeded {
		r.Log.Info("Replication has failed. Requeueing to retry.", "vrepName", vrepName)
		_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, func() (bool, error) {
			if _, annotationFound := r.VDB.Annotations[vmeta.OnlineUpgradeReplicatorAnnotation]; annotationFound {
				delete(r.VDB.Annotations, vmeta.OnlineUpgradeReplicatorAnnotation)
				return true, nil
			}
			return false, nil
		})
		if err != nil {
			r.Log.Error(err, "failed to delete replicator annotation in vdb", "annotation", vmeta.OnlineUpgradeReplicatorAnnotation)
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}
	r.Log.Info("Replication completed successfully", "vrepName", vrepName)
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

func (r *OnlineUpgradeReconciler) copyRedirectStateToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Skip if the sandbox has already been upgraded
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > promoteSandboxInx {
		return ctrl.Result{}, nil
	}

	mainPFacts := r.PFacts[vapi.MainCluster]
	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		r.Log.Error(err, "failed to gather podfacts for sandbox")
		return ctrl.Result{Requeue: true}, nil
	}
	mainInitiator, mainOK := mainPFacts.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	sbInitiator, sbOK := sbPFacts.FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !mainOK || !sbOK {
		r.Log.Info("No Up nodes found; requeueing reconciliation")
		return ctrl.Result{Requeue: true}, nil
	}

	sbSelectCmd := []string{"-tA", "-R", ",", "-c",
		"select concat(concat('''', id), '''') from v_internal_tables.v_redirect_state"}
	sbIds, stderr, err := mainPFacts.PRunner.ExecVSQL(ctx, sbInitiator.GetName(), names.ServerContainer, sbSelectCmd...)
	if err != nil {
		r.Log.Error(err, "failed to retrieve existing data from sandbox v_redirect_state table", "stderr", stderr)
		return ctrl.Result{Requeue: true}, nil
	}

	sql := "select * from v_internal_tables.v_redirect_state"
	if sbIds != "" {
		sql += fmt.Sprintf(" where id not in (%s)", sbIds)
	}
	selectCmd := []string{"-tA", "-F", ",", "-c", sql}
	rows, stderr, err := mainPFacts.PRunner.ExecVSQL(ctx, mainInitiator.GetName(), names.ServerContainer, selectCmd...)
	if err != nil {
		r.Log.Error(err, "failed to retrieve rows from main cluster v_redirect_state table", "stderr", stderr)
		return ctrl.Result{Requeue: true}, nil
	}

	for _, row := range strings.Split(strings.TrimSuffix(rows, "\n"), "\n") {
		if row == "" {
			continue
		}
		vals := ""
		for _, v := range strings.Split(row, ",") {
			vals += "'" + v + "',"
		}
		vals = strings.TrimSuffix(vals, ",")
		insertSQL := fmt.Sprintf("insert into v_internal_tables.v_redirect_state values (%s);", vals)
		insertCmd := []string{"-tAc", "select internal_tables_enable_edit('true'); " + insertSQL + " commit;"}
		_, stderr, err = sbPFacts.PRunner.ExecVSQL(ctx, sbInitiator.GetName(), names.ServerContainer, insertCmd...)
		if err != nil {
			r.Log.Error(err, "failed to insert data into v_redirect_state on sandbox", "stderr", stderr)
			return ctrl.Result{Requeue: true}, nil
		}
	}

	disableEditCmd := []string{"-tAc", "select internal_tables_enable_edit('false')"}
	_, stderr, err = sbPFacts.PRunner.ExecVSQL(ctx, sbInitiator.GetName(), names.ServerContainer, disableEditCmd...)
	if err != nil {
		r.Log.Error(err, "failed to disable internal table editing on sandbox", "stderr", stderr)
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// postRedirectToSandboxMsg will update the status message to indicate that
// we are diverting client connections to the sandbox now.
func (r *OnlineUpgradeReconciler) postRedirectToSandboxMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, redirectToSandboxMsgInx)
}

// redirectConnectionsToReplicaGroupB will redirect all of the connections
// established at replica group A to go to replica group B.
func (r *OnlineUpgradeReconciler) redirectConnectionsToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to monify redirect state on the old cluster
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > promoteSandboxInx {
		return ctrl.Result{}, nil
	}

	sbPFacts, err := r.getSandboxPodFacts(ctx, false)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Add the client routing labels to pods in the target subcluster. This
	// ensures the service object can reach them.  We use the podfacts for the
	// sandbox as we will always route to pods in the sandbox.
	actor := MakeClientRoutingLabelReconciler(r.VRec, r.Log, r.VDB, sbPFacts, AddNodeApplyMethod, "")
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	// then remove client routing labels from replica group a so no traffic is routed to the old main cluster
	actor = MakeClientRoutingLabelReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], DrainNodeApplyMethod, "")
	r.Manager.traceActorReconcile(actor)
	if res, err = actor.Reconcile(ctx, &ctrl.Request{}); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	initiator, ok := r.PFacts[vapi.MainCluster].FindFirstUpPod(false /*not allow read-only*/, "" /*arbitrary subcluster*/)
	if !ok {
		r.Log.Info("No Up nodes found; requeueing reconciliation")
		return ctrl.Result{Requeue: true}, nil
	}

	scMap := r.VDB.GenSubclusterMap()
	// after modifying routing to the sandbox, redirect existing connections to the sandbox too
	for i := range r.VDB.Spec.Subclusters {
		if r.VDB.Spec.Subclusters[i].Annotations[vmeta.ReplicaGroupAnnotation] != vmeta.ReplicaGroupBValue {
			continue
		}

		// scTarget is the subcluster in the sandbox to redirect connections to
		scTarget := &r.VDB.Spec.Subclusters[i]
		// scSource is the subcluster in the old main to redirect connections from
		scSource := scMap[scTarget.Annotations[vmeta.ParentSubclusterAnnotation]]
		tgtService, err := scTarget.GetService(ctx, r.VDB, r.VRec.Client)
		if err != nil {
			return ctrl.Result{}, err
		}
		var target string
		if tgtService.Spec.Type == corev1.ServiceTypeLoadBalancer {
			if len(tgtService.Status.LoadBalancer.Ingress) > 0 {
				ig := tgtService.Status.LoadBalancer.Ingress[0]
				if ig.IP != "" {
					target = ig.IP
				} else if ig.Hostname != "" {
					target = ig.Hostname
				} else {
					target = "127.0.0.1"
					r.Log.Info("failed to find hostname or ip for loadbalancer service, redirecting connections to localhost")
				}
			}
		} else if tgtService.Spec.Type == corev1.ServiceTypeExternalName {
			target = tgtService.Spec.ExternalName
		} else {
			// nodeport and clusterip both will function with this hostname
			target = fmt.Sprintf("%s.%s.svc.cluster.local", tgtService.Name, tgtService.Namespace)
		}
		// TODO: once server supports it, redirect with "connect to same host you did initially" for clients outside k8s
		err = r.Dispatcher.ManageConnectionDraining(ctx,
			manageconnectiondraining.WithSubcluster(scSource.Name), // redirect connections from scSource
			manageconnectiondraining.WithInitiator(initiator.GetPodIP()),
			manageconnectiondraining.WithAction(vclusterops.ActionRedirect),
			// redirect connections to the service associated with scTarget
			manageconnectiondraining.WithRedirectHostname(target),
		)
		if err != nil {
			r.Log.Error(err, "Failed to redirect connections; requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
	}

	return ctrl.Result{}, nil
}

// postPromoteSandboxMsg will update the status message to indicate that
// we are going to promote the sandbox to the main cluster now.
func (r *OnlineUpgradeReconciler) postPromoteSandboxMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, promoteSandboxMsgInx)
}

// promoteSandboxToMainCluster will promote the sandbox to the main cluster and
// discard the pods for the old main.
func (r *OnlineUpgradeReconciler) promoteSandboxToMainCluster(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to do this step
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > promoteSandboxInx {
		return ctrl.Result{}, nil
	}
	attempts := vmeta.GetOnlineUpgradePromotionAttempt(r.VDB.Annotations)
	if attempts >= vmeta.OnlineUpgradePromotionMaxAttempts {
		return r.logPromoteSandboxFailure()
	}

	sb := r.VDB.GetSandboxStatus(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, nil
	}
	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		return ctrl.Result{}, err
	}
	// All nodes in the sandbox must be up before sandbox promotion
	if sbPFacts.GetUpNodeCount() != len(sbPFacts.Detail) {
		r.Log.Info("Waiting for all pods in sandbox to be up for promotion.", "sandboxName", r.sandboxName)
		return ctrl.Result{Requeue: true}, nil
	}
	actor := MakePromoteSandboxToMainReconciler(r.VRec, r.Log, r.VDB, sbPFacts, r.Dispatcher, r.VRec.Client)
	r.Manager.traceActorReconcile(actor)
	var res ctrl.Result
	res, err = actor.Reconcile(ctx, &ctrl.Request{})
	// We want to increase the number of attempts only when there is an actual error during promotion
	if err != nil {
		attempts = vmeta.GetOnlineUpgradePromotionAttempt(r.VDB.Annotations) + 1
		anns := map[string]string{
			vmeta.OnlineUpgradePromotionAttemptAnnotation: strconv.Itoa(attempts),
		}
		_, err = vk8s.MetaUpdateWithAnnotations(ctx, r.VRec.GetClient(), r.VDB.ExtractNamespacedName(), r.VDB, anns)
		if err != nil {
			return ctrl.Result{}, err
		}
		return res, fmt.Errorf("attempt #%d: %w", attempts, err)
	}
	r.PFacts[vapi.MainCluster].Invalidate()
	r.Log.Info("sandbox has been promoted to main", "sandboxName", r.sandboxName)
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// deleteSandboxConfigMap deletes the sandbox(which is now the new main) configmap after
// the sandbox promotion.
func (r *OnlineUpgradeReconciler) deleteSandboxConfigMap(ctx context.Context) (ctrl.Result, error) {
	sb := r.VDB.GetSandboxStatus(r.sandboxName)
	if sb != nil {
		// We requeue if the sandbox still exists in the status
		return ctrl.Result{Requeue: true}, nil
	}
	sbMan := MakeSandboxConfigMapManager(r.VRec, r.VDB, r.sandboxName, "" /*no uuid*/)
	calledDelete, err := sbMan.deleteConfigMap(ctx)
	if !calledDelete {
		return ctrl.Result{}, err
	}
	if err != nil {
		r.Log.Error(err, "failed to delete sandbox config map", "configMapName", sbMan.configMap.Name)
		return ctrl.Result{}, err
	}
	r.Log.Info("deleted sandbox config map", "configMapName", sbMan.configMap.Name)
	return ctrl.Result{}, nil
}

func (r *OnlineUpgradeReconciler) waitForConnectionRedirect(ctx context.Context) (ctrl.Result, error) {
	timeout := vmeta.GetOnlineUpgradeTimeout(r.VDB.Annotations)
	// Iterate through the subclusters in replica group A. We check if there are
	// any active connections for each. Once they are all idle we can advance to
	// the next action in the upgrade.
	for i := 0; i < timeout; i++ {
		active := false
		for _, scName := range r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) {
			res, err := r.Manager.isSubclusterIdle(ctx, r.PFacts[vapi.MainCluster], scName)
			if err != nil {
				return res, err
			} else if res.Requeue {
				active = true
			}
		}
		if !active {
			return ctrl.Result{}, nil
		}
		time.Sleep(1 * time.Second)
	}

	return ctrl.Result{}, r.Manager.closeAllSessions(ctx, r.PFacts[vapi.MainCluster])
}

// postRemoveOriginalClusterMsg will update the status message to indicate that
// we are going to remove original_cluster/replica_group_a.
func (r *OnlineUpgradeReconciler) postRemoveOriginalClusterMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, removeOriginalClusterMsgInx)
}

// postRenameScsInMainClusterMsg will update the subcluster name in new main cluster.
// We will rename the subclusters in replica group B to the ones in replica group A.
func (r *OnlineUpgradeReconciler) postRenameScsInMainClusterMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, renameScsInMainClusterMsgInx)
}

// addNewSubclusters will come up with a list of subclusters we
// need to add to the VerticaDB to mimic the ones in the main cluster.
// The new subclusters will be added directly to r.VDB. This is a callback function for
// updateVDBWithRetry to prepare the vdb for update.
func (r *OnlineUpgradeReconciler) addNewSubclusters() (bool, error) {
	oldImage, found := r.Manager.fetchOldImage(vapi.MainCluster)
	if !found {
		return false, errors.New("Could not find old image needed for new subclusters")
	}
	newSubclusters := []vapi.Subcluster{}
	scMap := r.VDB.GenSubclusterMap()
	scSbMap := r.VDB.GenSubclusterSandboxMap()
	scsByType := []vapi.Subcluster{}
	for _, scName := range r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) {
		sc := scMap[scName]
		if sc == nil {
			return false, errors.New("Could not find some replica-group-a subclusters")
		}
		scsByType = append(scsByType, *sc)
	}
	// This will ensure that the primary of the sandbox is a copy
	// of a primary of the main cluster so we won't need to promote it
	sort.Slice(scsByType, func(i, j int) bool {
		return scsByType[i].Type < scsByType[j].Type
	})
	for i := range scsByType {
		sc := scMap[scsByType[i].Name]
		_, found := scSbMap[sc.Name]
		// we don't mimic a subcluster that is already in a sandbox
		if found {
			continue
		}
		newSCName, err := r.genNewSubclusterName(sc.Name, scMap)
		if err != nil {
			return false, err
		}

		newStsName, err := r.genNewSubclusterStsName(newSCName, sc)
		if err != nil {
			return false, err
		}

		newsc := r.duplicateSubclusterForReplicaGroupB(sc, newSCName, newStsName, oldImage)
		newSubclusters = append(newSubclusters, *newsc)
		scMap[newSCName] = newsc
	}

	if len(newSubclusters) == 0 {
		return false, errors.New("no subclusters found")
	}
	r.VDB.Spec.Subclusters = append(r.VDB.Spec.Subclusters, newSubclusters...)
	return true, nil
}

// assignSubclustersToReplicaGroupACallback is a callback method to update the
// VDB. It will assign each subcluster to replica group A by setting an
// annotation. This is a callback function for updatedVDBWithRetry to prepare
// the vdb for an update.
func (r *OnlineUpgradeReconciler) assignSubclustersToReplicaGroupACallback() (bool, error) {
	annotatedAtLeastOnce := false
	scSbMap := r.VDB.GenSubclusterSandboxStatusMap()
	for inx := range r.VDB.Spec.Subclusters {
		sc := &r.VDB.Spec.Subclusters[inx]
		// subclusters already in a sandbox must not be part of
		// replica group A.
		if _, found := scSbMap[sc.Name]; found {
			return annotatedAtLeastOnce, errors.New("Online upgrade cannot proceed if there is an existing sandbox")
		}
		if val, found := sc.Annotations[vmeta.ReplicaGroupAnnotation]; !found ||
			(val != vmeta.ReplicaGroupAValue && val != vmeta.ReplicaGroupBValue) {
			if sc.Annotations == nil {
				sc.Annotations = make(map[string]string, 1)
			}
			sc.Annotations[vmeta.ReplicaGroupAnnotation] = vmeta.ReplicaGroupAValue
			annotatedAtLeastOnce = true
		}
	}
	return annotatedAtLeastOnce, nil
}

// moveReplicaGroupBSubclusterToSandbox will move all subclusters attached to
// replica group B into the sandbox. This is a callback function for
// updateVDBWithRetry to prepare the vdb for an update.
func (r *OnlineUpgradeReconciler) moveReplicaGroupBSubclusterToSandbox() (bool, error) {
	oldImage, found := r.Manager.fetchOldImage(vapi.MainCluster)
	if !found {
		return false, errors.New("Could not find old image")
	}

	scNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	if len(scNames) == 0 {
		return false, errors.New("cound not find any subclusters for replica group B")
	}

	sandboxName, err := r.getNewSandboxName(preferredSandboxName)
	if err != nil {
		return false, fmt.Errorf("failed to generate a unique sandbox name: %w", err)
	}
	sandbox := vapi.Sandbox{
		Name:  sandboxName,
		Image: oldImage,
	}
	for _, nm := range scNames {
		sandbox.Subclusters = append(sandbox.Subclusters, vapi.SubclusterName{Name: nm})
	}
	r.VDB.Annotations[vmeta.OnlineUpgradeSandboxAnnotation] = sandboxName
	r.VDB.Spec.Sandboxes = append(r.VDB.Spec.Sandboxes, sandbox)
	return true, nil
}

// setImageInSandbox will set the new image in the sandbox to initiate an
// upgrade. This is a callback function for updateVDBWithRetry to prepare the
// vdb for update.
func (r *OnlineUpgradeReconciler) setImageInSandbox() (bool, error) {
	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return false, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}
	sb.Image = r.VDB.Spec.Image
	return true, nil
}

// countSubclustersForReplicaGroup is a helper to return the number of
// subclusters assigned to the given replica group.
func (r *OnlineUpgradeReconciler) countSubclustersForReplicaGroup(groupName string) int {
	scNames := r.VDB.GetSubclustersForReplicaGroup(groupName)
	return len(scNames)
}

// areGroupBSubclustersRenamed is a helper to check if all subclusters of replica group B
// have been renamed after sandbox promotion.
func (r *OnlineUpgradeReconciler) areGroupBSubclustersRenamed() bool {
	scNamesInGroupB := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	scs := r.VDB.GenSubclusterMap()
	for _, scName := range scNamesInGroupB {
		sc, found := scs[scName]
		if found && sc.Annotations[vmeta.ParentSubclusterAnnotation] != scName {
			return false
		}
	}
	return true
}

// genNewSubclusterName is a helper to generate a new subcluster name. The scMap
// passed in is used to test the uniqueness. It is up to the caller to update
// that map.
func (r *OnlineUpgradeReconciler) genNewSubclusterName(baseName string, scMap map[string]*vapi.Subcluster) (string, error) {
	// To make the name consistent, we will pick a standard suffix. If the
	// subcluster exists, then we will generate a random name based on the uid.
	// We do this only so that we can guess (in most cases) what the subcluster
	// name is for testing purposes.
	consistentName := fmt.Sprintf("%s-sb", baseName)
	if _, found := scMap[consistentName]; !found {
		return consistentName, nil
	}

	// Add a uuid suffix.
	return r.genNameWithUUID(baseName, func(nm string) bool { _, found := scMap[nm]; return found })
}

// genNewSubclusterStsName is a helper to generate the statefulset name of a new
// subcluster. It will return a unique name as determined by looking at all of
// the subclusters defined in the CR.
func (r *OnlineUpgradeReconciler) genNewSubclusterStsName(newSCName string, scToMimic *vapi.Subcluster) (string, error) {
	// Build up a map of all of the statefulset names defined for this database
	stsNameMap := make(map[string]bool)
	for i := range r.VDB.Spec.Subclusters {
		stsNameMap[r.VDB.Spec.Subclusters[i].GetStatefulSetName(r.VDB)] = true
	}

	// Preference is to match the name of the new subcluster.
	preferredStsName := fmt.Sprintf("%s-%s", r.VDB.Name, newSCName)
	// replace underscore to hypen for the statefulset name
	preferredStsName = v1beta1.GenCompatibleFQDNHelper(preferredStsName)
	if _, found := stsNameMap[preferredStsName]; !found {
		return preferredStsName, nil
	}

	// Then try using the original name of the subcluster. This may be available
	// if this the 2nd, 4th, etc. online upgrade. The sandbox will oscilate
	// between the name of the subcluster in the sandbox and its original name.
	nm := fmt.Sprintf("%s-%s", r.VDB.Name, scToMimic.Name)
	// replace underscore to hypen for the statefulset name
	nm = v1beta1.GenCompatibleFQDNHelper(nm)
	if _, found := stsNameMap[nm]; !found {
		return nm, nil
	}

	// Otherwise, generate a name using a uuid suffix
	return r.genNameWithUUID(preferredStsName,
		func(nm string) bool { _, found := stsNameMap[nm]; return found })
}

// getNewSandboxName returns a unique name to be used for a sandbox. A preferred
// name can be passed in. If that name is already in use, then we will generate
// a unique one using a UUID.
func (r *OnlineUpgradeReconciler) getNewSandboxName(preferredName string) (string, error) {
	sbNames := make(map[string]any)
	for i := range r.VDB.Spec.Sandboxes {
		sbNames[r.VDB.Spec.Sandboxes[i].Name] = true
	}

	// To make this easier to test, we will favor the annotation's value as the
	// sandbox name. If that's available that's our name.
	sbName := vmeta.GetOnlineUpgradePreferredSandboxName(r.VDB.Annotations)
	if _, found := sbNames[sbName]; sbName != "" && !found {
		return sbName, nil
	}

	// Add a uuid suffix to the preferred name.
	return r.genNameWithUUID(preferredName, func(nm string) bool { _, found := sbNames[nm]; return found })
}

// genNameWithUUID will return a unique name with a uuid suffix. The caller has
// to provide a lookup function to verify the name generated isn't used. If the
// lookupFunc returns true, that means the name is in use and another one will
// be generated.
func (r *OnlineUpgradeReconciler) genNameWithUUID(baseName string, lookupFunc func(nm string) bool) (string, error) {
	// Add a uuid suffix.
	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		nm := genBaseNameWithUUID(baseName, "-")
		if !lookupFunc(nm) {
			return nm, nil
		}
	}
	return "", errors.New("failed to generate a unique subcluster name")
}

// duplicateSubclusterForReplicaGroupB will return a new vapi.Subcluster that is based on
// baseSc. This is used to mimic the main cluster's subclusters in replica group B.
func (r *OnlineUpgradeReconciler) duplicateSubclusterForReplicaGroupB(
	baseSc *vapi.Subcluster, newSCName, newStsName, oldImage string) *vapi.Subcluster {
	newSc := baseSc.DeepCopy()
	newSc.Name = newSCName
	// The subcluster will be sandboxed. And only secondaries can be
	// sandbox.
	newSc.Type = vapi.SecondarySubcluster
	// Copy over the service name and all fields related to the service object.
	// They have to be the same. The client-routing label will be left off of
	// the sandbox pods. So, no traffic will hit them until they are added (see
	// MakeClientRoutingLabelReconciler).
	newSc.ServiceType = baseSc.ServiceType
	newSc.ClientNodePort = baseSc.ClientNodePort
	newSc.ExternalIPs = baseSc.ExternalIPs
	newSc.LoadBalancerIP = baseSc.LoadBalancerIP
	newSc.ServiceAnnotations = baseSc.ServiceAnnotations
	newSc.ServiceName = baseSc.GetServiceName()
	newSc.VerticaHTTPNodePort = baseSc.VerticaHTTPNodePort
	// The image in the vdb has already changed to the new one. We need to
	// set the image override so that the new subclusters come up with the
	// old image.
	newSc.ImageOverride = oldImage

	// Include annotations to indicate what replica group it is assigned to,
	// provide a link back to the subcluster it is defined from, and the desired
	// name of the subclusters statefulset.
	if newSc.Annotations == nil {
		newSc.Annotations = make(map[string]string)
	}
	newSc.Annotations[vmeta.ReplicaGroupAnnotation] = vmeta.ReplicaGroupBValue
	newSc.Annotations[vmeta.ParentSubclusterAnnotation] = baseSc.Name
	// When promoting this sc later we need to know the type of the subcluster
	// it mimics
	newSc.Annotations[vmeta.ParentSubclusterTypeAnnotation] = baseSc.Type
	// Picking a statefulset name is important because this subcluster will get
	// renamed later but we want a consistent object name to avoid having to
	// rebuild it.
	newSc.Annotations[vmeta.StsNameOverrideAnnotation] = newStsName

	// Create a linkage in the parent-child
	if baseSc.Annotations == nil {
		baseSc.Annotations = make(map[string]string)
	}
	return newSc
}

// postNextStatusMsg will set the next status message for a online upgrade
// according to msgIndex
func (r *OnlineUpgradeReconciler) postNextStatusMsg(ctx context.Context, msgIndex int) (ctrl.Result, error) {
	return ctrl.Result{}, r.Manager.postNextStatusMsg(ctx, onlineUpgradeStatusMsgs, msgIndex, vapi.MainCluster)
}

// getSandboxPodFacts returns a cached copy of the podfacts for the sandbox. If
// the podfacts aren't cached yet, it will cache them and optionally collect them.
func (r *OnlineUpgradeReconciler) getSandboxPodFacts(ctx context.Context, doCollection bool) (*podfacts.PodFacts, error) {
	// Collect the podfacts for the sandbox if not already done. We are going to
	// use the sandbox podfacts when we update the client routing label.
	if _, found := r.PFacts[r.sandboxName]; !found {
		sbPfacts := r.PFacts[vapi.MainCluster].Copy(r.sandboxName)
		r.PFacts[r.sandboxName] = &sbPfacts
	}
	if doCollection {
		r.PFacts[r.sandboxName].Invalidate()
		err := r.PFacts[r.sandboxName].Collect(ctx, r.VDB)
		if err != nil {
			return nil, fmt.Errorf("failed to collect podfacts for sandbox: %w", err)
		}
	}
	return r.PFacts[r.sandboxName], nil
}

// removeReplicaGroupAFromVdb will remove subclusters of replica group A from VerticaDB
func (r *OnlineUpgradeReconciler) removeReplicaGroupAFromVdb(ctx context.Context) (ctrl.Result, error) {
	// if the sandbox is still there, we wait for promote_sandbox to be done
	if r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	// if replica group A doesn't contain any subclustesr,
	// we skip removing the old main cluster
	if r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) == 0 {
		return ctrl.Result{}, nil
	}

	r.Log.Info("starting removal of replica group A from VerticaDB")

	scNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
	scNameSetForGroupA := make(map[string]any)
	for _, sc := range scNames {
		scNameSetForGroupA[sc] = struct{}{}
	}
	updateSubclustersInVdb := func() (bool, error) {
		// remove subclusters in replica group A
		removed := false
		for i := len(r.VDB.Spec.Subclusters) - 1; i >= 0; i-- {
			_, found := scNameSetForGroupA[r.VDB.Spec.Subclusters[i].Name]
			if found {
				r.VDB.Spec.Subclusters = append(r.VDB.Spec.Subclusters[:i], r.VDB.Spec.Subclusters[i+1:]...)
				removed = true
			}
		}
		return removed, nil
	}

	updated, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, updateSubclustersInVdb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete subclusters of old main cluster in vdb: %w", err)
	}
	if updated {
		r.Log.Info("deleted subclusters of old main cluster in vdb", "subclusters", scNames)
	}
	return ctrl.Result{}, nil
}

// removeReplicaGroupA will remove the old main cluster
func (r *OnlineUpgradeReconciler) removeReplicaGroupA(ctx context.Context) (ctrl.Result, error) {
	// if replica group A has already been removed, we skip removing the old main cluster
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > removeReplicaGroupAInx {
		return ctrl.Result{}, nil
	}
	// if the sandbox is still there, we wait for promote_sandbox to be done
	if r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	actor := MakeDBRemoveSubclusterReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster].PRunner,
		r.PFacts[vapi.MainCluster], r.Dispatcher, true)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// deleteReplicaGroupASts will delete the statefulSet of replicate group A.
func (r *OnlineUpgradeReconciler) deleteReplicaGroupASts(ctx context.Context) (ctrl.Result, error) {
	if vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) > deleteReplicaGroupAStsInx {
		return ctrl.Result{}, nil
	}
	// if the sandbox is still there, we wait for promote_sandbox to be done
	if r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{Requeue: true}, nil
	}

	actor := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

// renameReplicaGroupBFromVdb will rename the subclusters in promoted-sandbox/new-main-cluster to
// match the ones in original main cluster
func (r *OnlineUpgradeReconciler) renameReplicaGroupBFromVdb(ctx context.Context) (ctrl.Result, error) {
	// if replica group A still exists, we wait for remove_replica_group_A to be done
	if r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) > 0 {
		return ctrl.Result{Requeue: true}, nil
	}

	// if subclusters in replica group B have been renamed, we skip
	// renaming the subclusters in replica group B
	if r.areGroupBSubclustersRenamed() {
		return ctrl.Result{}, nil
	}

	err := r.PFacts[vapi.MainCluster].Collect(ctx, r.VDB)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to collect podfacts for main cluster: %w", err)
	}

	initiator, found := r.PFacts[vapi.MainCluster].FindFirstPrimaryUpPodIP()
	if !found {
		r.Log.Info("Requeue because there are no primary UP nodes in main cluster to execute rename-subcluster operation")
		return ctrl.Result{Requeue: true}, nil
	}

	scNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	scs := r.VDB.GenSubclusterMap()
	for _, scName := range scNames {
		sc, found := scs[scName]
		// this case shouldn't happen because we should be able to find the subcluster in vdb
		if !found {
			continue
		}
		// ignore the subcluster that has been renamed
		if sc.Annotations[vmeta.ParentSubclusterAnnotation] == scName {
			continue
		}
		newScName := sc.Annotations[vmeta.ParentSubclusterAnnotation]
		// rename the subcluster in vertica
		err = r.renameSubcluster(ctx, initiator, scName, newScName)
		if err != nil {
			return ctrl.Result{}, err
		}
		// rename the subcluster in vdb
		err = r.updateSubclusterNamesInVdb(ctx, scName, newScName)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// rename subclusters in sts
	actor := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, nil
}

// renameSubcluster will call vclusterOps to rename a subcluster in main cluster
func (r *OnlineUpgradeReconciler) renameSubcluster(ctx context.Context, initiator, scName, newScName string) error {
	opts := []renamesc.Option{
		renamesc.WithInitiator(initiator),
		renamesc.WithSubcluster(scName),
		renamesc.WithNewSubclusterName(newScName),
	}
	r.VRec.Eventf(r.VDB, corev1.EventTypeNormal, events.RenameSubclusterStart,
		"Starting rename subcluster %q to %q", scName, newScName)
	err := r.Dispatcher.RenameSubcluster(ctx, opts...)
	if err != nil {
		r.VRec.Eventf(r.VDB, corev1.EventTypeWarning, events.RenameSubclusterFailed,
			"Failed to rename subcluster %q to %q", scName, newScName)
		return err
	}
	r.VRec.Eventf(r.VDB, corev1.EventTypeNormal, events.RenameSubclusterSucceeded,
		"Successfully rename subcluster %q to %q", scName, newScName)

	return nil
}

// updateSubclusterNamesInVdb will update the names of subclusters in VerticaDB
func (r *OnlineUpgradeReconciler) updateSubclusterNamesInVdb(ctx context.Context, scName, newScName string) error {
	r.Log.Info("starting renaming subcluster in VerticaDB", "subcluster", scName, "new subcluster name", newScName)

	updateSubclustersInVdb := func() (bool, error) {
		// rename subcluster
		for i := range r.VDB.Spec.Subclusters {
			if r.VDB.Spec.Subclusters[i].Name == scName {
				r.VDB.Spec.Subclusters[i].Name = newScName
				return true, nil
			}
		}
		return false, nil
	}

	updated, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, updateSubclustersInVdb)
	if err != nil {
		return fmt.Errorf("failed to rename subcluster %q to %q in VerticaDB: %w", scName, newScName, err)
	}
	if updated {
		r.Log.Info("renamed subcluster in VerticaDB", "subcluster", scName, "new subcluster name", newScName)
	}
	return nil
}

// updateOnlineUpgradeStepAnnotation updates the annotation for vdb to indicate
// we have done a specific step in online upgrade
func (r *OnlineUpgradeReconciler) updateOnlineUpgradeStepAnnotation(ctx context.Context, inx int) error {
	anns := map[string]string{
		vmeta.OnlineUpgradeStepInxAnnotation: strconv.Itoa(inx),
	}
	_, err := vk8s.MetaUpdateWithAnnotations(ctx, r.VRec.GetClient(), r.VDB.ExtractNamespacedName(), r.VDB, anns)
	return err
}

// restartMainCluster calls restart reconciler on the main cluster
func (r *OnlineUpgradeReconciler) restartMainCluster(ctx context.Context) (ctrl.Result, error) {
	// reconciler to restart any down nodes.
	pf := r.PFacts[vapi.MainCluster]
	const DoNotRestartReadOnly = false
	actor := MakeRestartReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, DoNotRestartReadOnly, r.Dispatcher)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

func (r *OnlineUpgradeReconciler) createRestorePoint(ctx context.Context, pf *podfacts.PodFacts, archive string) (ctrl.Result, error) {
	res, err := r.Manager.createRestorePoint(ctx, pf, archive)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateOnlineUpgradeStepAnnotation(ctx, r.getNextStep())
}

func (r *OnlineUpgradeReconciler) getNextStep() int {
	return vmeta.GetOnlineUpgradeStepInx(r.VDB.Annotations) + 1
}

// logPromoteSandboxFailure logs a failure message if promotion has failed at least
// 3 times
func (r *OnlineUpgradeReconciler) logPromoteSandboxFailure() (ctrl.Result, error) {
	r.VRec.Eventf(r.VDB, corev1.EventTypeNormal, events.UpgradeFailed,
		"Online upgrade has failed after %d sandbox promotion attempts. "+
			"Please revive the database to its original state and try again.", vmeta.OnlineUpgradePromotionMaxAttempts)
	return ctrl.Result{}, fmt.Errorf("sandbox promotion failed at least %d times", vmeta.OnlineUpgradePromotionMaxAttempts)
}

func genBaseNameWithUUID(baseName, sep string) string {
	u := uuid.NewString()
	return fmt.Sprintf("%s%s%s", baseName, sep, u[0:5])
}
