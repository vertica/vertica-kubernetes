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

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
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

// List of status messages for online upgrade. When adding a new entry here,
// be sure to add a *StatusMsgInx const below.
var onlineUpgradeStatusMsgs = []string{
	"Starting online upgrade",
	"Create new subclusters to mimic subclusters in the main cluster",
	"Sandbox subclusters",
	"Promote secondaries whose base subcluster is primary",
	"Upgrade sandbox to new version",
	"Pause connections to main cluster",
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
	sandboxSubclustersMsgInx
	promoteSubclustersInSandboxMsgInx
	upgradeSandboxMsgInx
	pauseConnectionsMsgInx
	startReplicationMsgInx
	redirectToSandboxMsgInx
	promoteSandboxMsgInx
	removeOriginalClusterMsgInx
	renameScsInMainClusterMsgInx
)

// OnlineUpgradeReconciler will coordinate an online upgrade that allows
// write. This is done by splitting the cluster into two separate replicas and
// using failover strategies to keep the database online.
type OnlineUpgradeReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	VDB         *vapi.VerticaDB
	PFacts      map[string]*PodFacts // We have podfacts for main cluster and replica sandbox
	Manager     UpgradeManager
	Dispatcher  vadmin.Dispatcher
	sandboxName string // name of the sandbox created for replica group B
}

// MakeOnlineUpgradeReconciler will build a OnlineUpgradeReconciler object
func MakeOnlineUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &OnlineUpgradeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("OnlineUpgradeReconciler"),
		VDB:        vdb,
		PFacts:     map[string]*PodFacts{vapi.MainCluster: pfacts},
		Manager:    *MakeUpgradeManager(vdbrecon, log, vdb, vapi.OnlineUpgradeInProgress, onlineUpgradeAllowed),
		Dispatcher: dispatcher,
	}
}

// Reconcile will automate the process of a online upgrade.
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
		// Sandbox all of the secondary subclusters that are destined for
		// replica group B.
		r.postSandboxSubclustersMsg,
		r.sandboxReplicaGroupB,
		// Change replica b subcluster types to match the main cluster's
		r.postPromoteSubclustersInSandboxMsg,
		r.promoteReplicaBSubclusters,
		// Upgrade the version in the sandbox to the new version.
		r.postUpgradeSandboxMsg,
		r.upgradeSandbox,
		r.waitForSandboxUpgrade,
		// Pause all connections to replica A. This is to prepare for the
		// replication below.
		r.postPauseConnectionsMsg,
		r.pauseConnectionsAtReplicaGroupA,
		// Copy any new data that was added since the sandbox from replica group
		// A to replica group B.
		r.postStartReplicationMsg,
		r.startReplicationToReplicaGroupB,
		r.waitForReplicateToReplicaGroupB,
		// Redirect all of the connections to replica group A to replica group B.
		r.postRedirectToSandboxMsg,
		r.redirectConnectionsToReplicaGroupB,
		// Promote the sandbox to the main cluster and discard the pods for the
		// old main.
		r.postPromoteSandboxMsg,
		r.promoteSandboxToMainCluster,
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
	if vmeta.GetOnlineUpgradeReplicaARemoved(r.VDB.Annotations) == vmeta.ReplicaARemovedTrue ||
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
	rec := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// runAddSubclusterReconcilerForMainCluster will run the reconciler to create any necessary subclusters
func (r *OnlineUpgradeReconciler) runAddSubclusterReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddSubclusterReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// runAddNodesReconcilerForMainCluster will run the reconciler to scale out any subclusters.
func (r *OnlineUpgradeReconciler) runAddNodesReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddNodeReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// runRebalanceSandboxSubcluster will run a rebalance against the subclusters that will be sandboxed.
func (r *OnlineUpgradeReconciler) runRebalanceSandboxSubcluster(ctx context.Context) (ctrl.Result, error) {
	pf := r.PFacts[vapi.MainCluster]
	actor := MakeRebalanceShardsReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, "" /*all subclusters*/)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
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

// postSandboxSubclustersMsg will update the status message to indicate that
// we are going to sandbox subclusters for replica group b.
func (r *OnlineUpgradeReconciler) postSandboxSubclustersMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, sandboxSubclustersMsgInx)
}

// sandboxReplicaGroupB will move all of the subclusters in replica B to a new sandbox
func (r *OnlineUpgradeReconciler) sandboxReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// We can skip this step if the replica sandbox is already created and fully
	// sandboxed (according to status).
	if r.sandboxName != "" && r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{}, nil
	}

	// If we have already promoted sandbox to main, we don't need to sandbox replica B again
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
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

	// The nodes in the subcluster to sandbox must be running in order for
	// sandboxing to work. For this reason, we need to use the restart
	// reconciler to restart any down nodes.
	pf := r.PFacts[vapi.MainCluster]
	const DoNotRestartReadOnly = false
	actor := MakeRestartReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, DoNotRestartReadOnly, r.Dispatcher)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Drive the actual sandbox command. When this returns we know the sandbox is complete.
	actor = MakeSandboxSubclusterReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], r.Dispatcher, r.VRec.Client)
	r.Manager.traceActorReconcile(actor)
	res, err = actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	r.Log.Info("subclusters in replica group B have been sandboxed", "sandboxName", r.sandboxName)
	return ctrl.Result{}, nil
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
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
		return ctrl.Result{}, nil
	}

	sb := r.VDB.GetSandboxStatus(r.sandboxName)
	rgbSize := r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	if sb == nil || rgbSize != len(sb.Subclusters) {
		r.Log.Info("sandboxing replica group b is not complete")
		return ctrl.Result{Requeue: true}, nil
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
	// If we have already promoted sandbox to main, we don't need to upgrade the sandbox again
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
		return ctrl.Result{}, nil
	}

	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}

	// We can skip if the image in the sandbox matches the image in the vdb.
	// This is the new version that we are upgrading to.
	if sb.Image == r.VDB.Spec.Image {
		return ctrl.Result{}, nil
	}

	updated, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, r.setImageInSandbox)
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

	act := MakeSandboxUpgradeReconciler(r.VRec, r.Log, r.VDB)
	r.Manager.traceActorReconcile(act)
	return act.Reconcile(ctx, &ctrl.Request{})
}

// waitForSandboxUpgrade will wait for the sandbox upgrade to finish. It will
// continually check if the pods in the sandbox are up.
func (r *OnlineUpgradeReconciler) waitForSandboxUpgrade(ctx context.Context) (ctrl.Result, error) {
	// If we have already promoted sandbox to main, we don't need to wait for sandbox upgrade
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
		return ctrl.Result{}, nil
	}

	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("collected sandbox facts", "numPods", len(sbPFacts.Detail))
	for _, pf := range sbPFacts.Detail {
		r.Log.Info("sandbox pod fact", "pod", pf.name.Name, "image", pf.image, "up", pf.upNode)
		if pf.image != r.VDB.Spec.Image || !pf.upNode {
			r.Log.Info("Still waiting for sandbox to be upgraded")
			return ctrl.Result{Requeue: true}, nil
		}
	}
	return ctrl.Result{}, nil
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
	// If we have already promoted sandbox to main, we don't need to pause connections
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
		return ctrl.Result{}, nil
	}

	// In lieu of actual pause semantics, which will come later, we are going to
	// repurpose this step to close all existing sessions. We forcibly close all
	// connections as we want to prevent writes from happening. Continuing to
	// allow writes could potentially lead to data loss. We are about to
	// replicate the data, if writes can happen after the replication to replica
	// group B, they are going to be lost.
	//
	// We first need to route all traffic away from all subclusters in replica
	// group A. There is no target they will get routed too. Clients just won't
	// be able to connect until we finish the fail-over to replica group B. We
	// want do this for all pods that are in replica group A. The pod facts that
	// we pass in are for the main cluster, so that covers the pods we want to
	// do this for.
	actor := MakeClientRoutingLabelReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], DrainNodeApplyMethod, "")
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// close all existing user sessions
	err = r.Manager.closeAllSessions(ctx, r.PFacts[vapi.MainCluster])
	if err != nil {
		return ctrl.Result{}, err
	}

	// Iterate through the subclusters in replica group A. We check if there are
	// any active connections for each. Once they are all idle we can advance to
	// the next action in the upgrade.
	scNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
	for _, scName := range scNames {
		res, err := r.Manager.isSubclusterIdle(ctx, r.PFacts[vapi.MainCluster], scName)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// postStartReplicationMsg will update the status message to indicate that
// replication from the main to the sandbox is starting.
func (r *OnlineUpgradeReconciler) postStartReplicationMsg(ctx context.Context) (ctrl.Result, error) {
	return r.postNextStatusMsg(ctx, startReplicationMsgInx)
}

// startReplicationToReplicaGroupB will copy any new data that was added since
// the sandbox from replica group A to replica group B.
func (r *OnlineUpgradeReconciler) startReplicationToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
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
	vrepName := vmeta.GetOnlineUpgradeReplicator(r.VDB.Annotations)
	if vrepName == "" {
		r.Log.Info("skipping wait for VerticaReplicator because name cannot be found in vdb annotations")
		return ctrl.Result{}, nil
	}

	vrep := v1beta1.VerticaReplicator{}
	nm := types.NamespacedName{
		Name:      vrepName,
		Namespace: r.VDB.Namespace,
	}
	err := r.VRec.Client.Get(ctx, nm, &vrep)
	if err != nil {
		if kerrors.IsNotFound(err) {
			// Not found is okay since we'll delete the VerticaReplicator once
			// we see that the replication is finished.
			r.Log.Info("VerticaReplicator is not found. Skipping wait", "name", vrepName)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed trying to fetch VerticaReplicator: %w", err)
	}

	if !vrep.IsStatusConditionTrue(v1beta1.ReplicationComplete) {
		r.Log.Info("Requeue replication is not finished", "vrepName", vrepName)
		return ctrl.Result{Requeue: true}, nil
	}

	r.Log.Info("Replication is completed", "vrepName", vrepName)
	// Delete the VerticaReplicator. We leave the annotation present in the
	// VerticaDB so that we skip these steps until the upgrade is finished.
	err = r.VRec.Client.Delete(ctx, &vrep)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to delete the VerticaReplicator %s: %w", vrepName, err)
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
	// If we have already promoted sandbox to main, we don't need to redirect connections
	if vmeta.GetOnlineUpgradeSandboxPromoted(r.VDB.Annotations) == vmeta.SandboxPromotedTrue {
		return ctrl.Result{}, nil
	}

	// In lieu of the redirect, we are simply going to update the service object
	// to map to nodes in replica group B. There is no state to check to avoid
	// this function. The updates themselves are idempotent and will simply be
	// no-op if already done.
	//
	// Routing is easy for any primary subcluster since there is a duplicate
	// subcluster in replica group B. For secondaries, it is trickier. We need
	// to choose one of the subclusters created from replica group A's primary.
	// We will do a simple round robin for those ones.
	repAScNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)

	scMap := r.VDB.GenSubclusterMap()
	for _, scName := range repAScNames {
		sourceSc, found := scMap[scName]
		if !found {
			return ctrl.Result{}, fmt.Errorf("could not find subcluster %q in vdb", scName)
		}

		var targetScName string
		targetScName, found = sourceSc.Annotations[vmeta.ChildSubclusterAnnotation]
		if !found {
			return ctrl.Result{}, fmt.Errorf("could not find the %q annotation for the subcluster %q",
				vmeta.ChildSubclusterAnnotation, scName)
		}
		targetSc, found := scMap[targetScName]
		if !found {
			return ctrl.Result{}, fmt.Errorf("could not find subcluster %q in vdb", targetScName)
		}

		res, err := r.redirectConnectionsForSubcluster(ctx, sourceSc, targetSc)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
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
	sb := r.VDB.GetSandboxStatus(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, nil
	}
	sbPFacts, err := r.getSandboxPodFacts(ctx, false)
	if err != nil {
		return ctrl.Result{}, err
	}
	actor := MakePromoteSandboxToMainReconciler(r.VRec, r.Log, r.VDB, sbPFacts, r.Dispatcher, r.VRec.Client)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.PFacts[vapi.MainCluster].Invalidate()
	r.Log.Info("sandbox has been promoted to main", "sandboxName", r.sandboxName)
	return ctrl.Result{}, r.updateAnnotationForOnlineUpgrade(ctx, vmeta.OnlineUpgradeSandboxPromotedAnnotation)
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
	scsByType = append(scsByType, r.VDB.Spec.Subclusters...)
	// This will ensure that the primary of the sandbox is a copy
	// of a primary of the main cluster so we wo't need to promote it
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
	for inx := range r.VDB.Spec.Subclusters {
		sc := &r.VDB.Spec.Subclusters[inx]
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

// isGroupASubclusterInStatus is a helper to check if any subcluster of replica group A
// is in vdb status.
func (r *OnlineUpgradeReconciler) isGroupASubclusterInStatus() bool {
	scNames := r.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	scs := r.VDB.GenSubclusterMap()
	scNamesInA := []string{}
	// get subcluster names from annotations of subclusters in replica B because
	// subclusters in replica A have been removed from vdb.Spec
	for _, scName := range scNames {
		sc, found := scs[scName]
		if found && sc.Annotations[vmeta.ParentSubclusterAnnotation] != scName {
			scNamesInA = append(scNamesInA, sc.Annotations[vmeta.ParentSubclusterAnnotation])
		}
	}
	for _, scName := range scNamesInA {
		if r.VDB.IsSubclusterInStatus(scName) {
			return true
		}
	}
	return false
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
	nm := fmt.Sprintf("%s-%s", r.VDB.Name, newSCName)
	if _, found := stsNameMap[nm]; !found {
		return nm, nil
	}

	// Then try using the original name of the subcluster. This may be available
	// if this the 2nd, 4th, etc. online upgrade. The sandbox will oscilate
	// between the name of the subcluster in the sandbox and its original name.
	nm = fmt.Sprintf("%s-%s", r.VDB.Name, scToMimic.Name)
	if _, found := stsNameMap[nm]; !found {
		return nm, nil
	}

	// Otherwise, generate a name using a uuid suffix
	return r.genNameWithUUID(fmt.Sprintf("%s-%s", r.VDB.Name, newSCName),
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

	// To make this easier to test, we will favor the preferredName as the
	// sandbox name. If that's available that's our name.
	if _, found := sbNames[preferredName]; !found {
		return preferredName, nil
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
		u := uuid.NewString()
		nm := fmt.Sprintf("%s-%s", baseName, u[0:5])
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
	baseSc.Annotations[vmeta.ChildSubclusterAnnotation] = newSc.Name
	return newSc
}

// redirectConnectionsForSubcluster will update the service object so that
// connections for one subcluster get routed to another one. This will also set
// the client-routing label in the pod so that it can accept traffic.
func (r *OnlineUpgradeReconciler) redirectConnectionsForSubcluster(ctx context.Context, sourceSc, targetSc *vapi.Subcluster) (
	ctrl.Result, error) {
	r.Log.Info("Redirecting client connections", "source", sourceSc.Name, "target", targetSc.Name)
	if r.sandboxName == "" {
		return ctrl.Result{}, errors.New("sandbox name not cached")
	}

	sbPFacts, err := r.getSandboxPodFacts(ctx, true)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Add the client routing labels to pods in the target subcluster. This
	// ensures the service object can reach them.  We use the podfacts for the
	// sandbox as we will always route to pods in the sandbox.
	actor := MakeClientRoutingLabelReconciler(r.VRec, r.Log, r.VDB, sbPFacts,
		AddNodeApplyMethod, targetSc.Name)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// postNextStatusMsg will set the next status message for a online upgrade
// according to msgIndex
func (r *OnlineUpgradeReconciler) postNextStatusMsg(ctx context.Context, msgIndex int) (ctrl.Result, error) {
	return ctrl.Result{}, r.Manager.postNextStatusMsg(ctx, onlineUpgradeStatusMsgs, msgIndex, vapi.MainCluster)
}

// getSandboxPodFacts returns a cached copy of the podfacts for the sandbox. If
// the podfacts aren't cached yet, it will cache them and optionally collect them.
func (r *OnlineUpgradeReconciler) getSandboxPodFacts(ctx context.Context, doCollection bool) (*PodFacts, error) {
	// Collect the podfacts for the sandbox if not already done. We are going to
	// use the sandbox podfacts when we update the client routing label.
	if _, found := r.PFacts[r.sandboxName]; !found {
		sbPfacts := r.PFacts[vapi.MainCluster].Copy(r.sandboxName)
		r.PFacts[r.sandboxName] = &sbPfacts
	}
	if doCollection {
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
	// if the sandbox is still there, we wait for promote_sandbox to be done
	if r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	// if replica group A has removed, we skip removing the old main cluster
	if !r.isGroupASubclusterInStatus() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBRemoveSubclusterReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster].PRunner,
		r.PFacts[vapi.MainCluster], r.Dispatcher, true)
	r.Manager.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return ctrl.Result{}, r.updateAnnotationForOnlineUpgrade(ctx, vmeta.OnlineUpgradeReplicaARemovedAnnotation)
}

// deleteReplicaGroupASts will delete the statefulSet of replicate group A.
func (r *OnlineUpgradeReconciler) deleteReplicaGroupASts(ctx context.Context) (ctrl.Result, error) {
	// if the sandbox is still there, we wait for promote_sandbox to be done
	if r.VDB.GetSandboxStatus(r.sandboxName) != nil {
		return ctrl.Result{Requeue: true}, nil
	}
	// if replica group A has removed, we skip removing the old main cluster
	if !r.isGroupASubclusterInStatus() {
		return ctrl.Result{}, nil
	}

	actor := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
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
		err := r.renameSubcluster(ctx, initiator, scName, newScName)
		if err != nil {
			return ctrl.Result{}, err
		}
		// rename the subcluster in vdb
		err = r.updateSubclusterNamesInVdb(ctx, scName, newScName)
		if err != nil {
			return ctrl.Result{}, err
		}
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

var stepAnnotationWithValue = map[string]string{
	vmeta.OnlineUpgradeSandboxPromotedAnnotation: vmeta.SandboxPromotedTrue,
	vmeta.OnlineUpgradeReplicaARemovedAnnotation: vmeta.ReplicaARemovedTrue,
}

// updateAnnotationForOnlineUpgrade updates the annotation for vdb to indicate
// we have done a specific step in online upgrade
func (r *OnlineUpgradeReconciler) updateAnnotationForOnlineUpgrade(ctx context.Context, annotation string) error {
	value, found := stepAnnotationWithValue[annotation]
	if !found {
		return fmt.Errorf("annotation %q cannot be recognized", annotation)
	}
	updateAnnotation := func() (bool, error) {
		r.VDB.Annotations[annotation] = value
		return true, nil
	}

	updated, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.VDB, updateAnnotation)
	if err != nil {
		return fmt.Errorf("failed to update annotation %q in VerticaDB: %w", annotation, err)
	}
	if updated {
		r.Log.Info("updated annotation in VerticaDB", "annotation", annotation)
	}
	return nil
}
