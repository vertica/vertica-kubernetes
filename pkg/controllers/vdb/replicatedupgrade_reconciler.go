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

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReplicatedUpgradeReconciler will coordinate an online upgrade that allows
// write. This is done by splitting the cluster into two separate replicas and
// using failover strategies to keep the database online.
type ReplicatedUpgradeReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	VDB        *vapi.VerticaDB
	PFacts     *PodFacts
	Manager    UpgradeManager
	Dispatcher vadmin.Dispatcher
}

// MakeReplicatedUpgradeReconciler will build a ReplicatedUpgradeReconciler object
func MakeReplicatedUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &ReplicatedUpgradeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("ReplicatedUpgradeReconciler"),
		VDB:        vdb,
		PFacts:     pfacts,
		Manager:    *MakeUpgradeManager(vdbrecon, log, vdb, vapi.ReplicatedUpgradeInProgress, replicatedUpgradeAllowed),
		Dispatcher: dispatcher,
	}
}

// Reconcile will automate the process of a replicated upgrade.
func (r *ReplicatedUpgradeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if ok, err := r.Manager.IsUpgradeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := r.PFacts.Collect(ctx, r.VDB); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an upgrade by setting condition and event recording
		r.Manager.startUpgrade,
		// Load up state that is used for the subsequent steps
		r.loadSubclusterState,
		// Assign subclusters to upgrade to replica group A
		r.assignSubclustersToReplicaGroupA,
		r.runObjReconciler,
		// Create secondary subclusters for each of the primaries. These will be
		// added to replica group B and ready to be sandboxed.
		r.assignSubclustersToReplicaGroupB,
		r.runObjReconciler,
		r.runAddSubclusterReconciler,
		r.runAddNodesReconciler,
		// Sandbox all of the secondary subclusters that are destined for
		// replica group B.
		r.sandboxReplicaGroupB,
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

// loadSubclusterState will load state into the reconciler that
// is used in subsequent steps.
func (r *ReplicatedUpgradeReconciler) loadSubclusterState(ctx context.Context) (ctrl.Result, error) {
	err := r.Manager.cachePrimaryImages(ctx)
	return ctrl.Result{}, err
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

// runObjReconciler will run the object reconciler. This is used to build or
// update any necessary objects the upgrade depends on.
func (r *ReplicatedUpgradeReconciler) runObjReconciler(ctx context.Context) (ctrl.Result, error) {
	rec := MakeObjReconciler(r.VRec, r.Log, r.VDB, r.PFacts, ObjReconcileModeAll)
	return rec.Reconcile(ctx, &ctrl.Request{})
}

// runAddSubclusterReconciler will run the reconciler to create any necessary subclusters
func (r *ReplicatedUpgradeReconciler) runAddSubclusterReconciler(ctx context.Context) (ctrl.Result, error) {
	rec := MakeDBAddSubclusterReconciler(r.VRec, r.Log, r.VDB, r.PFacts.PRunner, r.PFacts, r.Dispatcher)
	return rec.Reconcile(ctx, &ctrl.Request{})
}

// runAddNodesReconciler will run the reconciler to scale out any subclusters.
func (r *ReplicatedUpgradeReconciler) runAddNodesReconciler(ctx context.Context) (ctrl.Result, error) {
	rec := MakeDBAddNodeReconciler(r.VRec, r.Log, r.VDB, r.PFacts.PRunner, r.PFacts, r.Dispatcher)
	return rec.Reconcile(ctx, &ctrl.Request{})
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
	return ctrl.Result{}, errors.New("sandbox of replica group B is not yet implemented")
}

// addNewSubclustersForPrimaries will come up with a list of subclusters we
// need to add to the VerticaDB to mimic the primaries. The new subclusters will
// be added directly to r.VDB.
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

		newsc := r.duplicateSubcluster(sc, newSCName, oldImage)
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
// annotation.
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

// countSubclustersForReplicaGroup is a helper to return the number of
// subclusters assigned to the given replica group.
func (r *ReplicatedUpgradeReconciler) countSubclustersForReplicaGroup(groupName string) int {
	count := 0
	for i := range r.VDB.Spec.Subclusters {
		if g, found := r.VDB.Spec.Subclusters[i].Annotations[vmeta.ReplicaGroupAnnotation]; found && g == groupName {
			count++
		}
	}
	return count
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
	const maxAttempts = 100
	for i := 0; i < maxAttempts; i++ {
		u := uuid.NewString()
		nm := fmt.Sprintf("%s-%s", baseName, u[0:5])
		if _, found := scMap[nm]; !found {
			return nm, nil
		}
	}
	return "", errors.New("failed to generate a unique subcluster name")
}

// duplicateSubcluster will return a new vapi.Subcluster that is based on
// baseSc. This is used to mimic the primaries in replica group B.
func (r *ReplicatedUpgradeReconciler) duplicateSubcluster(baseSc *vapi.Subcluster, newSCName, oldImage string) *vapi.Subcluster {
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
