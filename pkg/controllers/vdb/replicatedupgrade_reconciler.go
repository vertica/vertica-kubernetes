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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
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

const (
	// Entries into the status.upgradeState.replicas array for a particular replica group.
	replicaGroupA = 0
)

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
		// Assign subclusters to upgrade to replica group A
		r.assignSubclustersToReplicaGroups,
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

// assignSubclustersToReplicaGroups will go through all of the subclusters involved
// in the upgrade and assign them to the first replica group. The assignment is
// saved in the status.upgradeState.replicaGroups field.
func (r *ReplicatedUpgradeReconciler) assignSubclustersToReplicaGroups(ctx context.Context) (ctrl.Result, error) {
	// Early out if we have already assigned replica groups.
	if r.VDB.Status.UpgradeState != nil && len(r.VDB.Status.UpgradeState.ReplicaGroups) > 0 {
		return ctrl.Result{}, nil
	}

	// We simply assign all subclusters to the first group. This is used by
	// webhooks to prevent new subclusters being added that aren't part of the
	// upgrade.
	upgradeStatus := vapi.UpgradeState{
		ReplicaGroups: [][]string{{}, {}},
	}

	// Get the subcluster statefulsets. We sort this list so our algorithm for
	// replica group assignment is consistent.
	stss, err := r.Manager.Finder.FindStatefulSets(ctx, iter.FindExisting|iter.FindSorted)
	if err != nil {
		return ctrl.Result{}, err
	}

	for inx := range stss.Items {
		sts := &stss.Items[inx]

		scName, ok := sts.Labels[vmeta.SubclusterNameLabel]
		if !ok {
			return ctrl.Result{},
				fmt.Errorf("statefulset %q has missing subcluster name label %q", sts.Name, vmeta.SubclusterNameLabel)
		}

		upgradeStatus.ReplicaGroups[replicaGroupA] = append(upgradeStatus.ReplicaGroups[replicaGroupA], scName)
	}

	// Commit the replica groups to the status field for subsequent steps to pick up.
	err = vdbstatus.SetUpgradeState(ctx, r.VRec.Client, r.VDB, &upgradeStatus)
	return ctrl.Result{}, err
}
