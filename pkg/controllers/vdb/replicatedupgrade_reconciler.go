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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// When we generate a sandbox for the upgrade, this is preferred name of that sandbox.
const preferredSandboxName = "replica-group-b"

// ReplicatedUpgradeReconciler will coordinate an online upgrade that allows
// write. This is done by splitting the cluster into two separate replicas and
// using failover strategies to keep the database online.
type ReplicatedUpgradeReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	VDB         *vapi.VerticaDB
	PFacts      map[string]*PodFacts // We have podfacts for main cluster and replica sandbox
	Manager     UpgradeManager
	Dispatcher  vadmin.Dispatcher
	sandboxName string // name of the sandbox created for replica group B
}

// MakeReplicatedUpgradeReconciler will build a ReplicatedUpgradeReconciler object
func MakeReplicatedUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &ReplicatedUpgradeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("ReplicatedUpgradeReconciler"),
		VDB:        vdb,
		PFacts:     map[string]*PodFacts{vapi.MainCluster: pfacts},
		Manager:    *MakeUpgradeManager(vdbrecon, log, vdb, vapi.ReplicatedUpgradeInProgress, replicatedUpgradeAllowed),
		Dispatcher: dispatcher,
	}
}

// Reconcile will automate the process of a replicated upgrade.
func (r *ReplicatedUpgradeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if ok, err := r.Manager.IsUpgradeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := r.PFacts[vapi.MainCluster].Collect(ctx, r.VDB); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an upgrade by setting condition and event recording
		r.Manager.startUpgrade,
		// Load up state that is used for the subsequent steps
		r.loadUpgradeState,
		// Assign subclusters to upgrade to replica group A
		r.assignSubclustersToReplicaGroupA,
		// Create secondary subclusters for each of the primaries. These will be
		// added to replica group B and ready to be sandboxed.
		r.assignSubclustersToReplicaGroupB,
		r.runObjReconcilerForMainCluster,
		r.runAddSubclusterReconcilerForMainCluster,
		r.runAddNodesReconcilerForMainCluster,
		// Sandbox all of the secondary subclusters that are destined for
		// replica group B.
		r.sandboxReplicaGroupB,
		r.waitForSandboxToFinish,
		// Upgrade the version in the sandbox to the new version.
		r.upgradeSandbox,
		r.waitForSandboxUpgrade,
		// Pause all connections to replica A. This is to prepare for the
		// replication below.
		r.pauseConnectionsAtReplicaGroupA,
		// Copy any new data that was added since the sandbox from replica group
		// A to replica group B.
		r.startReplicationToReplicaGroupB,
		r.waitForReplicateToReplicaGroupB,
		// Redirect all of the connections to replica group A to replica group B.
		r.redirectConnectionsToReplicaGroupB,
		// Promote the sandbox to the main cluster and discard the pods for the
		// old main.
		r.promoteSandboxToMainCluster,
		// Scale-out secondary subcluster in main cluster. We will recreate the
		// secondary subcluster in replica group B that existed at the start of
		// the upgrade.
		r.scaleOutSecondariesInReplicaGroupB,
		// Cleanup up the condition and event recording for a completed upgrade
		r.Manager.finishUpgrade,
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

	return ctrl.Result{}, nil
}

// loadUpgradeState will load state into the reconciler that
// is used in subsequent steps.
func (r *ReplicatedUpgradeReconciler) loadUpgradeState(ctx context.Context) (ctrl.Result, error) {
	err := r.Manager.cachePrimaryImages(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.sandboxName = vmeta.GetReplicatedUpgradeSandbox(r.VDB.Annotations)
	return ctrl.Result{}, nil
}

// assignSubclustersToReplicaGroupA will go through all of the subclusters involved
// in the upgrade and assign them to the first replica group. The assignment is
// saved in the status.upgradeState.replicaGroups field.
func (r *ReplicatedUpgradeReconciler) assignSubclustersToReplicaGroupA(ctx context.Context) (ctrl.Result, error) {
	// Early out if we have already assigned replica groups.
	if r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue) != 0 {
		return ctrl.Result{}, nil
	}

	// We simply assign all subclusters to the first group. This is used by
	// webhooks to prevent new subclusters being added that aren't part of the
	// upgrade.
	_, err := r.Manager.updateVDBWithRetry(ctx, r.assignSubclustersToReplicaGroupACallback)
	return ctrl.Result{}, err
}

// runObjReconcilerForMainCluster will run the object reconciler for all objects
// that are part of the main cluster. This is used to build or update any
// necessary objects the upgrade depends on.
func (r *ReplicatedUpgradeReconciler) runObjReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	rec := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts[vapi.MainCluster], ObjReconcileModeAll)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// runAddSubclusterReconcilerForMainCluster will run the reconciler to create any necessary subclusters
func (r *ReplicatedUpgradeReconciler) runAddSubclusterReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddSubclusterReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// runAddNodesReconcilerForMainCluster will run the reconciler to scale out any subclusters.
func (r *ReplicatedUpgradeReconciler) runAddNodesReconcilerForMainCluster(ctx context.Context) (ctrl.Result, error) {
	pf := r.PFacts[vapi.MainCluster]
	rec := MakeDBAddNodeReconciler(r.VRec, r.Log, r.VDB, pf.PRunner, pf, r.Dispatcher)
	r.Manager.traceActorReconcile(rec)
	res, err := rec.Reconcile(ctx, &ctrl.Request{})
	r.PFacts[vapi.MainCluster].Invalidate()
	return res, err
}

// assignSubclustersToReplicaGroupB will figure out the subclusters that make up
// replica group B. We will add a secondary for each of the primaries that
// exist. This is a pre-step to setting up replica group B, which will
// eventually exist in its own sandbox.
func (r *ReplicatedUpgradeReconciler) assignSubclustersToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Early out if subclusters have already been assigned to replica group B.
	if r.countSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue) != 0 {
		return ctrl.Result{}, nil
	}

	updated, err := r.Manager.updateVDBWithRetry(ctx, r.addNewSubclustersForPrimaries)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed trying to update VDB with new subclusters: %w", err)
	}
	if updated {
		r.Log.Info("new secondary subclusters added to mimic the primaries", "len(subclusters)", len(r.VDB.Spec.Subclusters))
	}
	return ctrl.Result{}, nil
}

// sandboxReplicaGroupB will move all of the subclusters in replica B to a new sandbox
func (r *ReplicatedUpgradeReconciler) sandboxReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// We can skip this step if the replica sandbox is already created.
	if r.sandboxName != "" {
		return ctrl.Result{}, nil
	}

	_, err := r.Manager.updateVDBWithRetry(ctx, r.moveReplicaGroupBSubclusterToSandbox)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed trying to update VDB for sandboxing: %w", err)
	}
	r.sandboxName = vmeta.GetReplicatedUpgradeSandbox(r.VDB.Annotations)
	if r.sandboxName == "" {
		return ctrl.Result{}, errors.New("could not find sandbox name in annotations")
	}
	r.Log.Info("subclusters in replica group B have been sandboxed", "sandboxName", r.sandboxName)
	return ctrl.Result{}, nil
}

// waitForSandboxToFinish will block the upgrade until the sandbox controller has
// sandboxed the subclusters that are part of replica group B.
func (r *ReplicatedUpgradeReconciler) waitForSandboxToFinish(ctx context.Context) (ctrl.Result, error) {
	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}
	// If the image in the sandbox matches the image in the spec. We have
	// already attempted to upgrade the sandbox. So, this implies it has been
	// created already.
	if sb.Image == r.VDB.Spec.Image {
		return ctrl.Result{}, nil
	}

	// Check if we can skip this step if status already has the replicated
	// upgrade sandbox and the nodes are up.
	foundSandboxInStatus, err := r.isReplicatedUpgradeSandboxCreated()
	if err != nil {
		return ctrl.Result{}, err
	}

	if foundSandboxInStatus {
		r.Log.Info("sandboxes have been created")
		// We need to check pod facts of the sandbox to wait here for the nodes
		// in the sandbox to come up.
		return ctrl.Result{}, nil
	}
	// We will requeue for now. But eventually, we will call necessary
	// reconcilers here to drive the sandboxing.
	return ctrl.Result{Requeue: true}, nil
}

// upgradeSandbox will upgrade the nodes in replica group B (sandbox) to the new version.
func (r *ReplicatedUpgradeReconciler) upgradeSandbox(ctx context.Context) (ctrl.Result, error) {
	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return ctrl.Result{}, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}

	// We can skip if the image in the sandbox matches the image in the vdb.
	// This is the new version that we are upgrading to.
	if sb.Image == r.VDB.Spec.Image {
		return ctrl.Result{}, nil
	}

	updated, err := r.Manager.updateVDBWithRetry(ctx, r.setImageInSandbox)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed trying to update image in sandbox: %w", err)
	}
	if updated {
		r.Log.Info("update image in sandbox", "image", r.VDB.Spec.Image)
	}
	return ctrl.Result{}, nil
}

// waitForSandboxUpgrade will wait for the sandbox upgrade to finish. It will
// continually check if the pods in the sandbox are up.
func (r *ReplicatedUpgradeReconciler) waitForSandboxUpgrade(ctx context.Context) (ctrl.Result, error) {
	// The way I think we should do this is to get podfacts for the sandbox.
	// Then wait for the nodes to be up. Each time we find the nodes aren't up
	// we should requeue using the upgrade requeue time (see GetUpgradeRequeueTimeDuration).
	return ctrl.Result{}, errors.New("wait for sandbox upgrade is not yet implemented")
}

// pauseConnectionsAtReplicaGroupA will pause all connections to replica A. This
// is to prepare for the replication at the next step. We need to stop writes
// (momentarily) so that the two replica groups have the same data.
func (r *ReplicatedUpgradeReconciler) pauseConnectionsAtReplicaGroupA(ctx context.Context) (ctrl.Result, error) {
	// In lieu of actual pause semantics, which will come later, we are going to
	// repurpose this step to do an old style drain. We need all connections to
	// disconnect as we want to prevent writes from happening. Continuing to
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

	// Iterate through the subclusters in replica group A. We check if there are
	// any active connections for each. Once they are all idle we can advance to
	// the next action in the upgrade.
	scNames := r.getSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
	for _, scName := range scNames {
		res, err := r.Manager.isSubclusterIdle(ctx, r.PFacts[vapi.MainCluster], scName)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// startReplicationToReplicaGroupB will copy any new data that was added since
// the sandbox from replica group A to replica group B.
func (r *ReplicatedUpgradeReconciler) startReplicationToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// Skip if the VerticaReplicator has already been created.
	if vmeta.GetReplicatedUpgradeReplicator(r.VDB.Annotations) != "" {
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
		r.VDB.Annotations[vmeta.ReplicatedUpgradeReplicatorAnnotation] = vrep.Name
		return true, nil
	}
	_, err = r.Manager.updateVDBWithRetry(ctx, annotationUpdate)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to add replicator annotation to vdb: %w", err)
	}

	return ctrl.Result{}, nil
}

// waitForReplicateToReplicaGroupB will poll the VerticaReplicator waiting for the replication to finish.
func (r *ReplicatedUpgradeReconciler) waitForReplicateToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	vrepName := vmeta.GetReplicatedUpgradeReplicator(r.VDB.Annotations)
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

// redirectConnectionsToReplicaGroupB will redirect all of the connections
// established at replica group A to go to replica group B.
func (r *ReplicatedUpgradeReconciler) redirectConnectionsToReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	// In lieu of the redirect, we are simply going to update the service object
	// to map to nodes in replica group B. There is no state to check to avoid
	// this function. The updates themselves are idempotent and will simply be
	// no-op if already done.
	//
	// Routing is easy for any primary subcluster since there is a duplicate
	// subcluster in replica group B. For secondaries, it is trickier. We need
	// to choose one of the subclusters created from replica group A's primary.
	// We will do a simple round robin for those ones.
	repAScNames := r.getSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
	repBScNames := r.getSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	secondaryRoundRobinInx := 0

	scMap := r.VDB.GenSubclusterMap()
	for _, scName := range repAScNames {
		sourceSc, found := scMap[scName]
		if !found {
			return ctrl.Result{}, fmt.Errorf("could not find subcluster %q in vdb", scName)
		}

		var targetScName string
		if sourceSc.Type == vapi.PrimarySubcluster {
			// Easy for primary since there is a child subcluster created.
			targetScName, found = sourceSc.Annotations[vmeta.ChildSubclusterAnnotation]
			if !found {
				return ctrl.Result{}, fmt.Errorf("could not find the %q annotation for the subcluster %q",
					vmeta.ChildSubclusterAnnotation, scName)
			}
		} else {
			// For secondary, we will round robin among the subclusters defined in replica group B.
			targetScName = repBScNames[secondaryRoundRobinInx]
			secondaryRoundRobinInx++
			if secondaryRoundRobinInx >= len(repBScNames) {
				secondaryRoundRobinInx = 0
			}
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

// promoteSandboxToMainCluster will promote the sandbox to the main cluster and
// discard the pods for the old main.
func (r *ReplicatedUpgradeReconciler) promoteSandboxToMainCluster(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, errors.New("promote sandbox to main cluster is not yet implemented")
}

// Scale-out secondary subcluster in main cluster. We will recreate the
// secondary subcluster in replica group B that existed at the start of
// the upgrade.
func (r *ReplicatedUpgradeReconciler) scaleOutSecondariesInReplicaGroupB(ctx context.Context) (ctrl.Result, error) {
	if !r.VDB.HasSecondarySubclusters() {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, errors.New("scale out secondaries in replica group B is not yet implemented")
}

// addNewSubclustersForPrimaries will come up with a list of subclusters we
// need to add to the VerticaDB to mimic the primaries. The new subclusters will
// be added directly to r.VDB. This is a callback function for
// updateVDBWithRetry to prepare the vdb for update.
func (r *ReplicatedUpgradeReconciler) addNewSubclustersForPrimaries() (bool, error) {
	oldImage, found := r.Manager.fetchOldImage()
	if !found {
		return false, errors.New("Could not find old image needed for new subclusters")
	}
	newSubclusters := []vapi.Subcluster{}
	scMap := r.VDB.GenSubclusterMap()
	for i := range r.VDB.Spec.Subclusters {
		sc := &r.VDB.Spec.Subclusters[i]
		// We only mimic the primaries.
		if sc.Type != vapi.PrimarySubcluster {
			continue
		}

		newSCName, err := r.genNewSubclusterName(sc.Name, scMap)
		if err != nil {
			return false, err
		}

		newsc := r.duplicateSubclusterForReplicaGroupB(sc, newSCName, oldImage)
		newSubclusters = append(newSubclusters, *newsc)
		scMap[newSCName] = newsc
	}

	if len(newSubclusters) == 0 {
		return false, errors.New("no primary subclusters found")
	}
	r.VDB.Spec.Subclusters = append(r.VDB.Spec.Subclusters, newSubclusters...)
	return true, nil
}

// assignSubclustersToReplicaGroupACallback is a callback method to update the
// VDB. It will assign each subcluster to replica group A by setting an
// annotation. This is a callback function for updatedVDBWithRetry to prepare
// the vdb for an update.
func (r *ReplicatedUpgradeReconciler) assignSubclustersToReplicaGroupACallback() (bool, error) {
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
func (r *ReplicatedUpgradeReconciler) moveReplicaGroupBSubclusterToSandbox() (bool, error) {
	oldImage, found := r.Manager.fetchOldImage()
	if !found {
		return false, errors.New("Could not find old image")
	}

	scNames := r.getSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
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
	r.VDB.Annotations[vmeta.ReplicatedUpgradeSandboxAnnotation] = sandboxName
	r.VDB.Spec.Sandboxes = append(r.VDB.Spec.Sandboxes, sandbox)
	return true, nil
}

// setImageInSandbox will set the new image in the sandbox to initiate an
// upgrade. This is a callback function for updateVDBWithRetry to prepare the
// vdb for update.
func (r *ReplicatedUpgradeReconciler) setImageInSandbox() (bool, error) {
	sb := r.VDB.GetSandbox(r.sandboxName)
	if sb == nil {
		return false, fmt.Errorf("could not find sandbox %q", r.sandboxName)
	}
	sb.Image = r.VDB.Spec.Image
	return true, nil
}

// countSubclustersForReplicaGroup is a helper to return the number of
// subclusters assigned to the given replica group.
func (r *ReplicatedUpgradeReconciler) countSubclustersForReplicaGroup(groupName string) int {
	scNames := r.getSubclustersForReplicaGroup(groupName)
	return len(scNames)
}

// getSubclustersForReplicaGroup returns the names of the subclusters that are part of a replica group.
func (r *ReplicatedUpgradeReconciler) getSubclustersForReplicaGroup(groupName string) []string {
	scNames := []string{}
	for i := range r.VDB.Spec.Subclusters {
		if g, found := r.VDB.Spec.Subclusters[i].Annotations[vmeta.ReplicaGroupAnnotation]; found && g == groupName {
			scNames = append(scNames, r.VDB.Spec.Subclusters[i].Name)
		}
	}
	return scNames
}

// genNewSubclusterName is a helper to generate a new subcluster name. The scMap
// passed in is used to test the uniqueness. It is up to the caller to update
// that map.
func (r *ReplicatedUpgradeReconciler) genNewSubclusterName(baseName string, scMap map[string]*vapi.Subcluster) (string, error) {
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

// getNewSandboxName returns a unique name to be used for a sandbox. A preferred
// name can be passed in. If that name is already in use, then we will generate
// a unique one using a UUID.
func (r *ReplicatedUpgradeReconciler) getNewSandboxName(preferredName string) (string, error) {
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
func (r *ReplicatedUpgradeReconciler) genNameWithUUID(baseName string, lookupFunc func(nm string) bool) (string, error) {
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
// baseSc. This is used to mimic the primaries in replica group B.
func (r *ReplicatedUpgradeReconciler) duplicateSubclusterForReplicaGroupB(
	baseSc *vapi.Subcluster, newSCName, oldImage string) *vapi.Subcluster {
	newSc := baseSc.DeepCopy()
	newSc.Name = newSCName
	// The subcluster will be sandboxed. And only secondaries can be
	// sandbox.
	newSc.Type = vapi.SecondarySubcluster
	// We don't want to duplicate the service object settings. These new
	// subclusters will eventually reuse the service object of the primaries
	// they are mimicing. But not until they are ready to accept
	// connections. In the meantime, we will setup a simple ClusterIP style
	// service object. No one should really be connecting to them.
	newSc.ServiceType = corev1.ServiceTypeClusterIP
	newSc.ClientNodePort = 0
	newSc.ExternalIPs = nil
	newSc.LoadBalancerIP = ""
	newSc.ServiceAnnotations = nil
	newSc.ServiceName = ""
	newSc.VerticaHTTPNodePort = 0
	// The image in the vdb has already changed to the new one. We need to
	// set the image override so that the new subclusters come up with the
	// old image.
	newSc.ImageOverride = oldImage

	// Include annotations to indicate what replica group it is assigned to
	// and provide a link back to the subcluster it is defined from.
	if newSc.Annotations == nil {
		newSc.Annotations = make(map[string]string)
	}
	newSc.Annotations[vmeta.ReplicaGroupAnnotation] = vmeta.ReplicaGroupBValue
	newSc.Annotations[vmeta.ParentSubclusterAnnotation] = baseSc.Name

	// Create a linkage in the parent-child
	if baseSc.Annotations == nil {
		baseSc.Annotations = make(map[string]string)
	}
	baseSc.Annotations[vmeta.ChildSubclusterAnnotation] = newSc.Name
	return newSc
}

// isReplicatedUpgradeSandboxCreated will check that the sandbox created for
// replicated upgrade exists in the status.
func (r *ReplicatedUpgradeReconciler) isReplicatedUpgradeSandboxCreated() (bool, error) {
	scNames := r.getSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
	if len(scNames) == 0 {
		return false, errors.New("cound not find any subclusters for replica group B")
	}

	foundSandbox := false
	for i := range r.VDB.Status.Sandboxes {
		sb := r.VDB.Status.Sandboxes[i]
		if sb.Name != r.sandboxName {
			continue
		}
		foundSandbox = true

		// Sandbox was found. Do additional verification that the subclusters
		// contained within it are correct.
		if len(sb.Subclusters) != len(scNames) {
			return false, fmt.Errorf("wrong subcluster names found: %v vs %v", sb.Subclusters, scNames)
		}
		// Sort the subclusters names so that we can simply match entry i with
		// entry i.
		sort.Strings(scNames)
		sort.Strings(sb.Subclusters)
		for j := range sb.Subclusters {
			if sb.Subclusters[j] != scNames[j] {
				return false, fmt.Errorf("unexpected subcluster name found: %s vs %s", sb.Subclusters[j], scNames[j])
			}
		}
	}
	return foundSandbox, nil
}

// redirectConnectionsForSubcluster will update the service object so that
// connections for one subcluster get routed to another one. This will also set
// the client-routing label in the pod so that it can accept traffic.
func (r *ReplicatedUpgradeReconciler) redirectConnectionsForSubcluster(ctx context.Context, sourceSc, targetSc *vapi.Subcluster) (
	ctrl.Result, error) {
	if r.sandboxName == "" {
		return ctrl.Result{}, errors.New("sandbox name not cached")
	}

	selectorLabels := builder.MakeSvcSelectorLabelsForSubclusterNameRouting(r.VDB, targetSc)
	// The service object that we manipulate will always be from the main
	// cluster (ie. non-sandboxed).
	err := r.Manager.routeClientTraffic(ctx, r.PFacts[vapi.MainCluster], sourceSc, selectorLabels)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to update client routing for subcluster %q", sourceSc.Name)
	}

	// Collect the podfacts for the sandbox if not already done. We are going to
	// use the sandbox podfacts when we update the client routing label.
	if _, found := r.PFacts[r.sandboxName]; !found {
		sbPfacts := r.PFacts[vapi.MainCluster].Copy(r.sandboxName)
		r.PFacts[r.sandboxName] = &sbPfacts
	}
	err = r.PFacts[r.sandboxName].Collect(ctx, r.VDB)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to collect podfacts for sandbox: %w", err)
	}

	// Add the client routing labels to pods in the target subcluster. This
	// ensures the service object can reach them.  We use the podfacts for the
	// sandbox as we will always route to pods in the sandbox.
	actor := MakeClientRoutingLabelReconciler(r.VRec, r.Log, r.VDB, r.PFacts[r.sandboxName],
		AddNodeApplyMethod, targetSc.Name)
	r.Manager.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}
