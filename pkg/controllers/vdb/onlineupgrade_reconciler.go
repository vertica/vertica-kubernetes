/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OnlineUpgradeReconciler will handle the process when the vertica image
// changes.  It does this while keeping the database online.
type OnlineUpgradeReconciler struct {
	VRec          *VerticaDBReconciler
	Log           logr.Logger
	Vdb           *vapi.VerticaDB  // Vdb is the CRD we are acting on.
	TransientSc   *vapi.Subcluster // Set to the transient subcluster if applicable
	PRunner       cmds.PodRunner
	PFacts        *PodFacts
	Finder        iter.SubclusterFinder
	Manager       UpgradeManager
	PrimaryImages []string // Known images in the primaries.  Should be of length 1 or 2.
	StatusMsgs    []string // Precomputed status messages
	MsgIndex      int      // Current index in StatusMsgs
}

// MakeOnlineUpgradeReconciler will build an OnlineUpgradeReconciler object
func MakeOnlineUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &OnlineUpgradeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder:  iter.MakeSubclusterFinder(vdbrecon.Client, vdb),
		Manager: *MakeUpgradeManager(vdbrecon, log, vdb, vapi.OnlineUpgradeInProgress, onlineUpgradeAllowed),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OnlineUpgradeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Manager.IsUpgradeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an upgrade by setting condition and event recording
		o.Manager.startUpgrade,
		// Load up state that is used for the subsequent steps
		o.loadSubclusterState,
		// Figure out all of the status messages that we will report
		o.precomputeStatusMsgs,
		// Setup a transient subcluster to accept traffic when other subclusters
		// are down
		o.postNextStatusMsg,
		o.addTransientToVdb,
		o.createTransientSts,
		o.installTransientNodes,
		o.addTransientSubcluster,
		o.addTransientNodes,
		o.rebalanceTransientNodes,
		o.addClientRoutingLabelToTransientNodes,
		// Handle restart of the primary subclusters
		o.restartPrimaries,
		// Handle restart of secondary subclusters
		o.restartSecondaries,
		// Will cleanup the transient subcluster now that the primaries are back up.
		o.postNextStatusMsg,
		o.removeTransientFromVdb,
		o.removeClientRoutingLabelFromTransientNodes,
		o.removeTransientSubclusters,
		o.uninstallTransientNodes,
		o.deleteTransientSts,
		// Cleanup up the condition and event recording for a completed upgrade
		o.Manager.finishUpgrade,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); verrors.IsReconcileAborted(res, err) {
			// If Reconcile was aborted with a requeue, set the RequeueAfter interval to prevent exponential backoff
			if err == nil {
				res.Requeue = false
				res.RequeueAfter = o.Vdb.GetUpgradeRequeueTime()
			}
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// loadSubclusterState will load state into the OnlineUpgradeReconciler that
// is used in subsequent steps.
func (o *OnlineUpgradeReconciler) loadSubclusterState(ctx context.Context) (ctrl.Result, error) {
	var err error
	err = o.PFacts.Collect(ctx, o.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	o.TransientSc = o.Vdb.FindTransientSubcluster()

	err = o.cachePrimaryImages(ctx)
	return ctrl.Result{}, err
}

// precomputeStatusMsgs will figure out the status messages that we will use for
// the entire upgrade process.
func (o *OnlineUpgradeReconciler) precomputeStatusMsgs(ctx context.Context) (ctrl.Result, error) {
	o.StatusMsgs = []string{
		"Creating transient secondary subcluster",
		"Draining primary subclusters",
		"Recreating pods for primary subclusters",
		"Checking if new version is compatible",
		"Waiting for secondary nodes to become read-only",
		"Restarting vertica in primary subclusters",
	}

	// Function we call for each secondary subcluster
	procFunc := func(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
		scName := sts.Labels[builder.SubclusterNameLabel]
		o.StatusMsgs = append(o.StatusMsgs,
			fmt.Sprintf("Draining secondary subcluster '%s'", scName),
			fmt.Sprintf("Recreating pods for secondary subcluster '%s'", scName),
			fmt.Sprintf("Restarting vertica in secondary subcluster '%s'", scName),
		)
		return ctrl.Result{}, nil
	}
	if res, err := o.iterateSubclusterType(ctx, vapi.SecondarySubclusterType, procFunc); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	o.StatusMsgs = append(o.StatusMsgs, "Destroying transient secondary subcluster")
	o.MsgIndex = -1
	return ctrl.Result{}, nil
}

// postNextStatusMsg will set the next status message for an online upgrade
func (o *OnlineUpgradeReconciler) postNextStatusMsg(ctx context.Context) (ctrl.Result, error) {
	o.MsgIndex++
	return ctrl.Result{}, o.Manager.postNextStatusMsg(ctx, o.StatusMsgs, o.MsgIndex)
}

// postNextStatusMsgForSts will set the next status message for the online image
// change.  This version is meant to be called for a specific statefulset.  This
// exists just to have the proper function signature.  We ignore the sts
// entirely as the status message for the sts is already precomputed.
func (o *OnlineUpgradeReconciler) postNextStatusMsgForSts(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	return o.postNextStatusMsg(ctx)
}

// addTransientToVdb will add the transient subcluster to the VerticaDB.  This
// is stored in the api server.  It will get removed at the end of the
// upgrade.
func (o *OnlineUpgradeReconciler) addTransientToVdb(ctx context.Context) (ctrl.Result, error) {
	if o.TransientSc != nil {
		return ctrl.Result{}, nil
	}

	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	oldImage, ok := o.fetchOldImage()
	if !ok {
		return ctrl.Result{}, fmt.Errorf("could not determine the old image name.  "+
			"Only available image is %s", o.Vdb.Spec.Image)
	}

	transientSc := o.Vdb.BuildTransientSubcluster(oldImage)

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := o.Vdb.ExtractNamespacedName()
		if err := o.VRec.Client.Get(ctx, nm, o.Vdb); err != nil {
			return err
		}

		// Ensure we only have at most one transient subcluster
		if otherSc := o.Vdb.FindTransientSubcluster(); otherSc != nil {
			o.Log.Info("Transient subcluster already exists.  Skip adding another one",
				"name", otherSc.Name)
			o.TransientSc = otherSc // Ensure we cache the one we found
			return nil
		}

		o.Vdb.Spec.Subclusters = append(o.Vdb.Spec.Subclusters, *transientSc)
		o.TransientSc = &o.Vdb.Spec.Subclusters[len(o.Vdb.Spec.Subclusters)-1]
		err := o.VRec.Client.Update(ctx, o.Vdb)
		if err != nil {
			return err
		}

		// Refresh things now that vdb has changed
		o.PFacts.Invalidate()
		o.Finder = iter.MakeSubclusterFinder(o.VRec.Client, o.Vdb)
		return nil
	})
	return ctrl.Result{}, err
}

// createTransientSts this will create a secondary subcluster to accept
// traffic from subclusters when they are down.  This subcluster is called
// the transient and only exist for the life of the upgrade.
func (o *OnlineUpgradeReconciler) createTransientSts(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts, ObjReconcileModeAll)
	o.traceActorReconcile(actor)
	or := actor.(*ObjReconciler)

	return or.reconcileSts(ctx, o.TransientSc)
}

// installTransientNodes will ensure we have installed vertica on
// each of the nodes in the transient subcluster.
func (o *OnlineUpgradeReconciler) installTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeInstallReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// addTransientSubcluster will register a new transient subcluster with Vertica
func (o *OnlineUpgradeReconciler) addTransientSubcluster(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBAddSubclusterReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	d := actor.(*DBAddSubclusterReconciler)
	return d.addMissingSubclusters(ctx, []vapi.Subcluster{*o.TransientSc})
}

// addTransientNodes will ensure nodes on the transient have been added to the
// cluster.
func (o *OnlineUpgradeReconciler) addTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBAddNodeReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	d := actor.(*DBAddNodeReconciler)
	return d.reconcileSubcluster(ctx, o.TransientSc)
}

// rebalanceTransientNodes will run a rebalance against the transient subcluster
func (o *OnlineUpgradeReconciler) rebalanceTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeRebalanceShardsReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, o.TransientSc.Name)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// addClientRoutingLabelToTransientNodes will add the special routing label so
// that Service objects can use the transient subcluster.
func (o *OnlineUpgradeReconciler) addClientRoutingLabelToTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeClientRoutingLabelReconciler(o.VRec, o.Vdb, o.PFacts, AddNodeApplyMethod, o.TransientSc.Name)
	o.traceActorReconcile(actor)
	// Add the labels.  If there is a node that still has missing subscriptions
	// that will fail with requeue error.
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// iterateSubclusterType will iterate over the subclusters, calling the
// processFunc for each one that matches the given type.
func (o *OnlineUpgradeReconciler) iterateSubclusterType(ctx context.Context, scType string,
	processFunc func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error)) (ctrl.Result, error) {
	stss, err := o.Finder.FindStatefulSets(ctx, iter.FindExisting|iter.FindSorted)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range stss.Items {
		sts := &stss.Items[i]
		if matches, err := o.isMatchingSubclusterType(sts, scType); err != nil {
			return ctrl.Result{}, err
		} else if !matches {
			continue
		}

		if res, err := processFunc(ctx, sts); verrors.IsReconcileAborted(res, err) {
			o.Log.Info("Error during subcluster iteration", "res", res, "err", err)
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// restartPrimaries will handle the upgrade on all of the primaries.
func (o *OnlineUpgradeReconciler) restartPrimaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of primaries")

	funcs := []func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error){
		o.drainSubcluster,
		o.recreateSubclusterWithNewImage,
		o.checkVersion,
		o.waitForReadOnly,
		o.bringSubclusterOnline,
	}
	for i, fn := range funcs {
		if res, err := o.postNextStatusMsg(ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		if res, err := o.iterateSubclusterType(ctx, vapi.PrimarySubclusterType, fn); verrors.IsReconcileAborted(res, err) {
			o.Log.Info("Error iterating subclusters over function", "i", i)
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// restartSecondaries will restart all of the secondaries, temporarily
// rerouting traffic to the transient while it does the restart.
func (o *OnlineUpgradeReconciler) restartSecondaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of secondaries")
	res, err := o.iterateSubclusterType(ctx, vapi.SecondarySubclusterType, o.processSecondary)
	return res, err
}

// processSecondary will handle restart of a single secondary subcluster
func (o *OnlineUpgradeReconciler) processSecondary(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	funcs := []func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error){
		o.postNextStatusMsgForSts,
		o.drainSubcluster,
		o.postNextStatusMsgForSts,
		o.recreateSubclusterWithNewImage,
		o.postNextStatusMsgForSts,
		o.bringSubclusterOnline,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx, sts); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// isMatchingSubclusterType will return true if the subcluster type matches the
// input string.  Always returns false for the transient subcluster.
func (o *OnlineUpgradeReconciler) isMatchingSubclusterType(sts *appsv1.StatefulSet, scType string) (bool, error) {
	isTransient, err := strconv.ParseBool(sts.Labels[builder.SubclusterTransientLabel])
	if err != nil {
		return false, fmt.Errorf("could not parse label %s: %w", builder.SubclusterTransientLabel, err)
	}
	return sts.Labels[builder.SubclusterTypeLabel] == scType && !isTransient, nil
}

// drainSubcluster will reroute traffic away from a subcluster and wait for it to be idle.
// This is a no-op if the image has already been updated for the subcluster.
func (o *OnlineUpgradeReconciler) drainSubcluster(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	img := sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image

	if img != o.Vdb.Spec.Image {
		scName := sts.Labels[builder.SubclusterNameLabel]
		o.Log.Info("rerouting client traffic from subcluster", "name", scName)
		if err := o.routeClientTraffic(ctx, scName, true); err != nil {
			return ctrl.Result{}, err
		}

		o.Log.Info("starting check for active connections in subcluster", "name", scName)
		return o.isSubclusterIdle(ctx, scName)
	}
	return ctrl.Result{}, nil
}

// recreateSubclusterWithNewImage will recreate the subcluster so that it runs with the
// new image.
func (o *OnlineUpgradeReconciler) recreateSubclusterWithNewImage(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	var err error

	stsChanged, err := o.Manager.updateImageInStatefulSet(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stsChanged {
		o.PFacts.Invalidate()
	}

	scName := sts.Labels[builder.SubclusterNameLabel]
	podsDeleted, err := o.Manager.deletePodsRunningOldImage(ctx, scName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if podsDeleted > 0 {
		o.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}

func (o *OnlineUpgradeReconciler) checkVersion(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	if o.Vdb.Spec.IgnoreUpgradePath {
		return ctrl.Result{}, nil
	}

	const EnforceUpgradePath = true
	a := MakeVersionReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, EnforceUpgradePath)

	// We use a custom lookup function to only find pods for the subcluster we
	// are working on.
	vr := a.(*VersionReconciler)
	scName := sts.Labels[builder.SubclusterNameLabel]
	vr.FindPodFunc = func() (*PodFact, bool) {
		for _, v := range o.PFacts.Detail {
			if v.isPodRunning && v.subcluster == scName {
				return v, true
			}
		}
		return &PodFact{}, false
	}
	return vr.Reconcile(ctx, &ctrl.Request{})
}

// waitForReadOnly will only succeed if all of the up pods running the old image
// are in read-only state.  This wait is necessary so that we don't try to do a
// 'AT -t restart_node' for the primary nodes when the cluster is in read-only.
// We should always start those with 'AT -t start_db'.
func (o *OnlineUpgradeReconciler) waitForReadOnly(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	// Early out if the primaries have restarted.  This wait is only meant to be
	// done after we take down the primaries and are waiting for spread to move
	// the remaining up nodes into read-only.
	if o.PFacts.countUpPrimaryNodes() != 0 {
		return ctrl.Result{}, nil
	}
	newImage := sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image
	// If all the pods that are running the old image are read-only we are done
	// our wait.
	if o.PFacts.countNotReadOnlyWithOldImage(newImage) == 0 {
		return ctrl.Result{}, nil
	}
	o.Log.Info("Requeueing because at least 1 pod running the old image is still up and isn't considered read-only yet")
	return ctrl.Result{Requeue: true}, nil
}

// bringSubclusterOnline will bring up a subcluster and reroute traffic back to the subcluster.
func (o *OnlineUpgradeReconciler) bringSubclusterOnline(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	const DoNotRestartReadOnly = false
	actor := MakeRestartReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, DoNotRestartReadOnly)
	o.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	scName := sts.Labels[builder.SubclusterNameLabel]

	actor = MakeClientRoutingLabelReconciler(o.VRec, o.Vdb, o.PFacts, PodRescheduleApplyMethod, scName)
	res, err = actor.Reconcile(ctx, &ctrl.Request{})
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	o.Log.Info("starting client traffic routing back to subcluster", "name", scName)
	err = o.routeClientTraffic(ctx, scName, false)
	return ctrl.Result{}, err
}

// removeTransientFromVdb will remove the transient subcluster that is in the VerticaDB stored in the apiserver
func (o *OnlineUpgradeReconciler) removeTransientFromVdb(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}

	o.Log.Info("starting removal of transient from VerticaDB")

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := o.Vdb.ExtractNamespacedName()
		if err := o.VRec.Client.Get(ctx, nm, o.Vdb); err != nil {
			return err
		}

		// Remove the transient.
		removedTransient := false
		for i := len(o.Vdb.Spec.Subclusters) - 1; i >= 0; i-- {
			if o.Vdb.Spec.Subclusters[i].IsTransient {
				o.Vdb.Spec.Subclusters = append(o.Vdb.Spec.Subclusters[:i], o.Vdb.Spec.Subclusters[i+1:]...)
				removedTransient = true
			}
		}
		if !removedTransient {
			return nil
		}
		o.PFacts.Invalidate() // Force refresh due to transient being removed
		o.TransientSc = nil
		return o.VRec.Client.Update(ctx, o.Vdb)
	})
	return ctrl.Result{}, err
}

// removeClientRoutingLabelFromTransientNodes will remove the special routing
// label since we are about to remove that subcluster
func (o *OnlineUpgradeReconciler) removeClientRoutingLabelFromTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}

	actor := MakeClientRoutingLabelReconciler(o.VRec, o.Vdb, o.PFacts, DelNodeApplyMethod, "")
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// removeTransientSubclusters will drive subcluster removal of the transient subcluster.
func (o *OnlineUpgradeReconciler) removeTransientSubclusters(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBRemoveSubclusterReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// uninstallTransientNodes will drive uninstall logic for any transient nodes.
func (o *OnlineUpgradeReconciler) uninstallTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}
	actor := MakeUninstallReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// deleteTransientSts will delete the transient subcluster that was created for the upgrade.
func (o *OnlineUpgradeReconciler) deleteTransientSts(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts, ObjReconcileModeAll)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// cachePrimaryImages will update o.PrimaryImages with the names of all of the primary images
func (o *OnlineUpgradeReconciler) cachePrimaryImages(ctx context.Context) error {
	stss, err := o.Finder.FindStatefulSets(ctx, iter.FindExisting)
	if err != nil {
		return err
	}
	for i := range stss.Items {
		sts := &stss.Items[i]
		if sts.Labels[builder.SubclusterTypeLabel] == vapi.PrimarySubclusterType {
			img := sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image
			imageFound := false
			for j := range o.PrimaryImages {
				imageFound = o.PrimaryImages[j] == img
				if imageFound {
					break
				}
			}
			if !imageFound {
				o.PrimaryImages = append(o.PrimaryImages, img)
			}
		}
	}
	return nil
}

// fetchOldImage will return the old image that existed prior to the image
// change process.  If we cannot determine the old image, then the bool return
// value returns false.
func (o *OnlineUpgradeReconciler) fetchOldImage() (string, bool) {
	for i := range o.PrimaryImages {
		if o.PrimaryImages[i] != o.Vdb.Spec.Image {
			return o.PrimaryImages[i], true
		}
	}
	return "", false
}

// skipTransientSetup will return true if we can skip creation, install and
// scale-out of the transient subcluster
func (o *OnlineUpgradeReconciler) skipTransientSetup() bool {
	// We can skip this entirely if all of the primary subclusters already have
	// the new image.  This is an indication that we have already created the
	// transient and did the image change.
	if !o.Vdb.RequiresTransientSubcluster() || (len(o.PrimaryImages) == 1 && o.PrimaryImages[0] == o.Vdb.Spec.Image) {
		return true
	}

	// We skip creating the transient if the cluster is down.  We cannot add the
	// transient if everything is down.  And there is nothing "online" with this
	// upgrade if we start with everything down.
	_, found := o.PFacts.findPodToRunVsql(false, "")
	return !found
}

func (o *OnlineUpgradeReconciler) traceActorReconcile(actor controllers.ReconcileActor) {
	o.Log.Info("starting actor for online upgrade", "name", fmt.Sprintf("%T", actor))
}

// routeClientTraffic will update service objects to route to either the primary
// or transient.  The subcluster picked is determined by the scCheckFunc the
// caller provides.  If it returns true for a given subcluster, traffic will be
// routed to that.
func (o *OnlineUpgradeReconciler) routeClientTraffic(ctx context.Context,
	scName string, setTemporaryRouting bool) error {
	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts, ObjReconcileModeAll)
	objRec := actor.(*ObjReconciler)

	scMap := o.Vdb.GenSubclusterMap()
	sc, ok := scMap[scName]
	if !ok {
		return fmt.Errorf("we are routing for a subcluster that isn't in the vdb: %s", scName)
	}

	// We update the external service object to route traffic to transient or
	// primary/secondary.  We are only concerned with changing the labels.  So
	// we will fetch the current service object, then update the labels so that
	// traffic diverted to the correct statefulset.  Other things, such as
	// service type, stay the same.
	svcName := names.GenExtSvcName(o.Vdb, sc)
	svc := &corev1.Service{}
	if err := o.VRec.Client.Get(ctx, svcName, svc); err != nil {
		if errors.IsNotFound(err) {
			o.Log.Info("Skipping client traffic routing because service object for subcluster not found",
				"scName", scName, "svc", svcName)
			return nil
		}
		return err
	}

	// If we are to set temporary routing, we are going to route traffic
	// to a transient subcluster (if one exists) or to a subcluster
	// defined in the vdb.
	if setTemporaryRouting {
		routingSc := o.getSubclusterForTemporaryRouting(ctx, sc, scMap)
		if routingSc != nil {
			svc.Spec.Selector = builder.MakeSvcSelectorLabelsForSubclusterNameRouting(o.Vdb, routingSc)
		}
	} else {
		svc.Spec.Selector = builder.MakeSvcSelectorLabelsForServiceNameRouting(o.Vdb, sc)
	}
	o.Log.Info("Updating svc", "selector", svc.Spec.Selector)
	return objRec.reconcileExtSvc(ctx, svc, sc)
}

// getSubclusterForTemporaryRouting returns a pointer to a subcluster to use for
// temporary routing.  If no routing decision could be made, this will return nil.
func (o *OnlineUpgradeReconciler) getSubclusterForTemporaryRouting(ctx context.Context,
	offlineSc *vapi.Subcluster, scMap map[string]*vapi.Subcluster) *vapi.Subcluster {
	if o.TransientSc != nil {
		transientSc := o.TransientSc

		// Only continue if the transient subcluster exists. It may not
		// exist if the entire cluster was down when we attempted to create it.
		transientSts := &appsv1.StatefulSet{}
		stsName := names.GenStsName(o.Vdb, transientSc)
		if err := o.VRec.Client.Get(ctx, stsName, transientSts); err != nil {
			if errors.IsNotFound(err) {
				o.Log.Info("Skipping routing to transient since it does not exist", "name", stsName)
				return nil
			}
			return nil
		}
		return transientSc
	}

	var routingSc *vapi.Subcluster

	// If no subcluster routing is specified, we will pick existing subclusters.
	if len(o.Vdb.Spec.TemporarySubclusterRouting.Names) == 0 {
		return o.pickDefaultSubclusterForTemporaryRouting(offlineSc)
	}

	// Pick one of the subclusters defined in Names.  We pick the first one that
	// isn't currently being taken offline.
	for i := range o.Vdb.Spec.TemporarySubclusterRouting.Names {
		routeName := o.Vdb.Spec.TemporarySubclusterRouting.Names[i]
		sc, ok := scMap[routeName]
		if !ok {
			o.Log.Info("Temporary routing subcluster not found.  Skipping", "Name", routeName)
			continue
		}
		routingSc = sc

		// Keep searching if we are routing to the subcluster we are taking
		// offline.  We may continue with this subcluster still if no other
		// subclusters are defined -- this is why we updated the svc object
		// with it.
		if routeName != offlineSc.Name {
			return routingSc
		}
	}
	return routingSc
}

// pickDefaultSubclusterForTemporaryRouting will pick a suitable default for
// temporary routing.  This is called when the temporarySubclusterRouting field
// in the CR is empty.
func (o *OnlineUpgradeReconciler) pickDefaultSubclusterForTemporaryRouting(offlineSc *vapi.Subcluster) *vapi.Subcluster {
	// When we take down primaries, we take all of the primaries.  So we pick
	// the first secondary we can find.  If there are no secondaries, then
	// selecting the first subcluster will do.  The upgrade won't be online in
	// this case, but there isn't anything we can do.
	if offlineSc.IsPrimary {
		for i := range o.Vdb.Spec.Subclusters {
			sc := &o.Vdb.Spec.Subclusters[i]
			if !sc.IsPrimary {
				return sc
			}
		}
		return &o.Vdb.Spec.Subclusters[0]
	}

	// If taking down a secondary, we pick the first non-matching subcluster.
	// That subcluster we pick should be up, only the offlineSc subcluster will
	// be down.
	for i := range o.Vdb.Spec.Subclusters {
		sc := &o.Vdb.Spec.Subclusters[i]
		if sc.Name != offlineSc.Name {
			return sc
		}
	}
	return nil
}

// isSubclusterIdle will run a query to see the number of connections
// that are active for a given subcluster.  It returns a requeue error if there
// are active connections still.
func (o *OnlineUpgradeReconciler) isSubclusterIdle(ctx context.Context, scName string) (ctrl.Result, error) {
	pf, ok := o.PFacts.findPodToRunVsql(true, scName)
	if !ok {
		o.Log.Info("No pod found to run vsql.  Skipping active connection check")
		return ctrl.Result{}, nil
	}

	sql := fmt.Sprintf(
		"select count(session_id) sessions"+
			" from v_monitor.sessions join v_catalog.subclusters using (node_name)"+
			" where session_id not in (select session_id from current_session)"+
			"       and subcluster_name = '%s';", scName)

	cmd := []string{"-tAc", sql}
	stdout, _, err := o.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Parse the output.  We requeue if there is an active connection.  This
	// will rely on the UpgradeRequeueTime that is set to default
	return ctrl.Result{Requeue: anyActiveConnections(stdout)}, nil
}

// anyActiveConnections will parse the output from vsql to see if there
// are any active connections.  Returns true if there is at least one
// connection.
func anyActiveConnections(stdout string) bool {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	// As a convience for test, allow empty string to be treated as having no
	// active connections.
	return res != "" && res != "0"
}
