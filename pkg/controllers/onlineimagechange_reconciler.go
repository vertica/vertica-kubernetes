/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OnlineImageChangeReconciler will handle the process when the vertica image
// changes.  It does this while keeping the database online.
type OnlineImageChangeReconciler struct {
	VRec          *VerticaDBReconciler
	Log           logr.Logger
	Vdb           *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner       cmds.PodRunner
	PFacts        *PodFacts
	Finder        SubclusterFinder
	Manager       ImageChangeManager
	PrimaryImages []string // Known images in the primaries.  Should be of length 1 or 2.
	StatusMsgs    []string // Precomputed status messages
	MsgIndex      int      // Current index in StatusMsgs
}

// MakeOnlineImageChangeReconciler will build an OnlineImageChangeReconciler object
func MakeOnlineImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &OnlineImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder:  MakeSubclusterFinder(vdbrecon.Client, vdb),
		Manager: *MakeImageChangeManager(vdbrecon, log, vdb, vapi.OnlineImageChangeInProgress, onlineImageChangeAllowed),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OnlineImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Manager.IsImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		o.Manager.startImageChange,
		// Load up state that is used for the subsequent steps
		o.loadSubclusterState,
		// Figure out all of the status messages that we will report
		o.precomputeStatusMsgs,
		// Setup a transient subcluster to accept traffic when other subclusters
		// are down
		o.postNextStatusMsg,
		o.createTransientSts,
		o.installTransientNodes,
		o.addTransientSubcluster,
		o.addTransientNodes,
		o.waitForReadyTransientPod,
		// Handle restart of the primary subclusters
		o.restartPrimaries,
		// Handle restart of secondary subclusters
		o.restartSecondaries,
		// Will cleanup the transient subcluster now that the primaries are back up.
		o.postNextStatusMsg,
		o.removeTransientSubclusters,
		o.uninstallTransientNodes,
		o.deleteTransientSts,
		// Cleanup up the condition and event recording for a completed image change
		o.Manager.finishImageChange,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// loadSubclusterState will load state into the OnlineImageChangeReconciler that
// is used in subsequent steps.
func (o *OnlineImageChangeReconciler) loadSubclusterState(ctx context.Context) (ctrl.Result, error) {
	var err error
	err = o.PFacts.Collect(ctx, o.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = o.cachePrimaryImages(ctx)
	return ctrl.Result{}, err
}

// precomputeStatusMsgs will figure out the status messages that we will use for
// the entire image change process.
func (o *OnlineImageChangeReconciler) precomputeStatusMsgs(ctx context.Context) (ctrl.Result, error) {
	o.StatusMsgs = []string{
		"Creating transient secondary subcluster",
		"Draining primary subclusters",
		"Recreating pods for primary subclusters",
		"Checking if new version is compatible",
		"Restarting vertica in primary subclusters",
	}

	// Function we call for each secondary subcluster
	procFunc := func(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
		scName := sts.Labels[SubclusterNameLabel]
		o.StatusMsgs = append(o.StatusMsgs,
			fmt.Sprintf("Draining secondary subcluster '%s'", scName),
			fmt.Sprintf("Recreating pods for secondary subcluster '%s'", scName),
			fmt.Sprintf("Restarting vertica in secondary subcluster '%s'", scName),
		)
		return ctrl.Result{}, nil
	}
	if res, err := o.iterateSubclusterType(ctx, vapi.SecondarySubclusterType, procFunc); res.Requeue || err != nil {
		return res, err
	}
	o.StatusMsgs = append(o.StatusMsgs, "Destroying transient secondary subcluster")
	o.MsgIndex = -1
	return ctrl.Result{}, nil
}

// postNextStatusMsg will set the next status message for an online image change
func (o *OnlineImageChangeReconciler) postNextStatusMsg(ctx context.Context) (ctrl.Result, error) {
	o.MsgIndex++
	return ctrl.Result{}, o.Manager.postNextStatusMsg(ctx, o.StatusMsgs, o.MsgIndex)
}

// postNextStatusMsgForSts will set the next status message for the online image
// change.  This version is meant to be called for a specific statefulset.  This
// exists just to have the proper function signature.  We ignore the sts
// entirely as the status message for the sts is already precomputed.
func (o *OnlineImageChangeReconciler) postNextStatusMsgForSts(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	return o.postNextStatusMsg(ctx)
}

// createTransientSts this will create a secondary subcluster to accept
// traffic from subclusters when they are down.  This subcluster is called
// the transient and only exist for the life of the image change.
func (o *OnlineImageChangeReconciler) createTransientSts(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	o.traceActorReconcile(actor)
	or := actor.(*ObjReconciler)

	oldImage, ok := o.fetchOldImage()
	if !ok {
		return ctrl.Result{}, fmt.Errorf("could not determine the old image name.  "+
			"Only available image is %s", o.Vdb.Spec.Image)
	}

	sc := buildTransientSubcluster(o.Vdb, oldImage)
	return or.reconcileSts(ctx, sc)
}

// installTransientNodes will ensure we have installed vertica on
// each of the nodes in the transient subcluster.
func (o *OnlineImageChangeReconciler) installTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeInstallReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// addTransientSubcluster will register a new transient subcluster with Vertica
func (o *OnlineImageChangeReconciler) addTransientSubcluster(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBAddSubclusterReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	d := actor.(*DBAddSubclusterReconciler)
	return d.addMissingSubclusters(ctx, []vapi.Subcluster{*buildTransientSubcluster(o.Vdb, "")})
}

// addTransientNodes will ensure nodes on the transient have been added to the
// cluster.
func (o *OnlineImageChangeReconciler) addTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBAddNodeReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	d := actor.(*DBAddNodeReconciler)
	return d.reconcileSubcluster(ctx, buildTransientSubcluster(o.Vdb, ""))
}

// waitForReadyTransientPod will wait for one of the transient pods to be ready.
// This is done so that when we direct traffic to the transient subcluster the
// service object has a pod to route too.
func (o *OnlineImageChangeReconciler) waitForReadyTransientPod(ctx context.Context) (ctrl.Result, error) {
	pod := &corev1.Pod{}
	sc := buildTransientSubcluster(o.Vdb, "")
	// We only check the first pod is ready
	pn := names.GenPodName(o.Vdb, sc, 0)

	const MaxAttempts = 30 // Retry for roughly 30 seconds
	for i := 0; i < MaxAttempts; i++ {
		if err := o.VRec.Client.Get(ctx, pn, pod); err != nil {
			// Any error, including not found, aborts the retry.  The pod should
			// have already existed because we call this after db add node.  The
			// transient pod is not restartable, so if the pod isn't running,
			// then it won't ever be ready.
			o.Log.Info("Error while fetching transient pod", "err", err)
			return ctrl.Result{}, nil
		}
		if pod.Status.ContainerStatuses[ServerContainerIndex].Ready {
			o.Log.Info("Transient pod is in ready state",
				"containerStatuses", pod.Status.ContainerStatuses[ServerContainerIndex])
			return ctrl.Result{}, nil
		}
		const AttemptSleepTime = 1
		time.Sleep(AttemptSleepTime * time.Second)
	}
	// If we timeout, we still continue on.  The transient pod is not
	// restartable, so we don't want to wait indefinitely.  The image change
	// will proceed but any routing to the transient pods will fail.
	return ctrl.Result{}, nil
}

// iterateSubclusterType will iterate over the subclusters, calling the
// processFunc for each one that matches the given type.
func (o *OnlineImageChangeReconciler) iterateSubclusterType(ctx context.Context, scType string,
	processFunc func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error)) (ctrl.Result, error) {
	stss, err := o.Finder.FindStatefulSets(ctx, FindExisting|FindSorted)
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

		if res, err := processFunc(ctx, sts); res.Requeue || err != nil {
			o.Log.Info("Error during subcluster iteration", "res", res, "err", err)
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// restartPrimaries will handle the image change on all of the primaries.
func (o *OnlineImageChangeReconciler) restartPrimaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of primaries")

	funcs := []func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error){
		o.drainSubcluster,
		o.recreateSubclusterWithNewImage,
		o.checkVersion,
		o.bringSubclusterOnline,
	}
	for i, fn := range funcs {
		if res, err := o.postNextStatusMsg(ctx); res.Requeue || err != nil {
			return res, err
		}
		if res, err := o.iterateSubclusterType(ctx, vapi.PrimarySubclusterType, fn); res.Requeue || err != nil {
			o.Log.Info("Error iterating subclusters over function", "i", i)
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// restartSecondaries will restart all of the secondaries, temporarily
// rerouting traffic to the transient while it does the restart.
func (o *OnlineImageChangeReconciler) restartSecondaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of secondaries")
	res, err := o.iterateSubclusterType(ctx, vapi.SecondarySubclusterType, o.processSecondary)
	return res, err
}

// processSecondary will handle restart of a single secondary subcluster
func (o *OnlineImageChangeReconciler) processSecondary(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	funcs := []func(context.Context, *appsv1.StatefulSet) (ctrl.Result, error){
		o.postNextStatusMsgForSts,
		o.drainSubcluster,
		o.postNextStatusMsgForSts,
		o.recreateSubclusterWithNewImage,
		o.postNextStatusMsgForSts,
		o.bringSubclusterOnline,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx, sts); res.Requeue || err != nil {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// isMatchingSubclusterType will return true if the subcluster type matches the
// input string.  Always returns false for the transient subcluster.
func (o *OnlineImageChangeReconciler) isMatchingSubclusterType(sts *appsv1.StatefulSet, scType string) (bool, error) {
	isTransient, err := strconv.ParseBool(sts.Labels[SubclusterTransientLabel])
	if err != nil {
		return false, fmt.Errorf("could not parse label %s: %w", SubclusterTransientLabel, err)
	}
	return sts.Labels[SubclusterTypeLabel] == scType && !isTransient, nil
}

// drainSubcluster will reroute traffic away from a subcluster and wait for it to be idle.
// This is a no-op if the image has already been updated for the subcluster.
func (o *OnlineImageChangeReconciler) drainSubcluster(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	img := sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image

	if img != o.Vdb.Spec.Image {
		scName := sts.Labels[SubclusterNameLabel]
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
func (o *OnlineImageChangeReconciler) recreateSubclusterWithNewImage(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	var err error

	stsChanged, err := o.Manager.updateImageInStatefulSet(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}
	if stsChanged {
		o.PFacts.Invalidate()
	}

	scName := sts.Labels[SubclusterNameLabel]
	podsDeleted, err := o.Manager.deletePodsRunningOldImage(ctx, scName)
	if err != nil {
		return ctrl.Result{}, err
	}
	if podsDeleted > 0 {
		o.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}

func (o *OnlineImageChangeReconciler) checkVersion(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	if o.Vdb.Spec.IgnoreUpgradePath {
		return ctrl.Result{}, nil
	}

	const EnforceUpgradePath = true
	a := MakeVersionReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, EnforceUpgradePath)

	// We use a custom lookup function to only find pods for the subcluster we
	// are working on.
	vr := a.(*VersionReconciler)
	scName := sts.Labels[SubclusterNameLabel]
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

// bringSubclusterOnline will bring up a subcluster and reroute traffic back to the subcluster.
func (o *OnlineImageChangeReconciler) bringSubclusterOnline(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	const DoNotRestartReadOnly = false
	actor := MakeRestartReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, DoNotRestartReadOnly)
	o.traceActorReconcile(actor)
	res, err := actor.Reconcile(ctx, &ctrl.Request{})
	if res.Requeue || err != nil {
		return res, err
	}
	o.PFacts.Invalidate() // Status of the pods may have changed

	scName := sts.Labels[SubclusterNameLabel]
	o.Log.Info("starting client traffic routing back to subcluster", "name", scName)
	err = o.routeClientTraffic(ctx, scName, false)
	return ctrl.Result{}, err
}

// removeTransientSubclusters will drive subcluster removal of the transient subcluster
func (o *OnlineImageChangeReconciler) removeTransientSubclusters(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}
	actor := MakeDBRemoveSubclusterReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// uninstallTransientNodes will drive uninstall logic for any transient nodes.
func (o *OnlineImageChangeReconciler) uninstallTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}
	actor := MakeUninstallReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// deleteTransientSts will delete the transient subcluster that was created for the image change.
func (o *OnlineImageChangeReconciler) deleteTransientSts(ctx context.Context) (ctrl.Result, error) {
	if !o.Vdb.RequiresTransientSubcluster() {
		return ctrl.Result{}, nil
	}

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// cachePrimaryImages will update o.PrimaryImages with the names of all of the primary images
func (o *OnlineImageChangeReconciler) cachePrimaryImages(ctx context.Context) error {
	stss, err := o.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return err
	}
	for i := range stss.Items {
		sts := &stss.Items[i]
		if sts.Labels[SubclusterTypeLabel] == vapi.PrimarySubclusterType {
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
func (o *OnlineImageChangeReconciler) fetchOldImage() (string, bool) {
	for i := range o.PrimaryImages {
		if o.PrimaryImages[i] != o.Vdb.Spec.Image {
			return o.PrimaryImages[i], true
		}
	}
	return "", false
}

// skipTransientSetup will return true if we can skip creation, install and
// scale-out of the transient subcluster
func (o *OnlineImageChangeReconciler) skipTransientSetup() bool {
	// We can skip this entirely if all of the primary subclusters already have
	// the new image.  This is an indication that we have already created the
	// transient and done the image change.
	if !o.Vdb.RequiresTransientSubcluster() || (len(o.PrimaryImages) == 1 && o.PrimaryImages[0] == o.Vdb.Spec.Image) {
		return true
	}

	// We skip creating the transient if the cluster is down.  We cannot add the
	// transient if everything is down.  And there is nothing "online" with this
	// image change if we start with everything down.
	_, found := o.PFacts.findPodToRunVsql(false, "")
	return !found
}

func (o *OnlineImageChangeReconciler) traceActorReconcile(actor ReconcileActor) {
	o.Log.Info("starting actor for online image change", "name", fmt.Sprintf("%T", actor))
}

// routeClientTraffic will update service objects to route to either the primary
// or transient.  The subcluster picked is determined by the scCheckFunc the
// caller provides.  If it returns true for a given subcluster, traffic will be
// routed to that.
func (o *OnlineImageChangeReconciler) routeClientTraffic(ctx context.Context,
	scName string, setTemporaryRouting bool) error {
	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
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
		foundRoutingSubcluster := false
		for i := range o.Vdb.Spec.TemporarySubclusterRouting.Names {
			routeName := o.Vdb.Spec.TemporarySubclusterRouting.Names[i]
			routingSc, ok := scMap[routeName]
			if !ok {
				o.Log.Info("Temporary routing subcluster not found.  Skipping", "Name", routeName)
				continue
			}
			svc.Spec.Selector = makeSvcSelectorLabelsForSubclusterNameRouting(o.Vdb, routingSc)
			foundRoutingSubcluster = true

			// Keep searching if we are routing to the subcluster we are taking
			// offline.  We may continue with this subcluster still if no other
			// subclusters are defined -- this is why we updated the svc object
			// with it.
			if routeName == scName {
				continue
			}
			break
		}
		if !foundRoutingSubcluster {
			// We are modifying a copy of sc, so we set the IsTransient flag to
			// know what subcluster we are going to route to.
			transientSc := buildTransientSubcluster(o.Vdb, "")

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

			svc.Spec.Selector = makeSvcSelectorLabelsForSubclusterNameRouting(o.Vdb, transientSc)
		}
	} else {
		svc.Spec.Selector = makeSvcSelectorLabelsForServiceNameRouting(o.Vdb, sc)
	}
	o.Log.Info("Updating svc", "selector", svc.Spec.Selector)
	return objRec.reconcileExtSvc(ctx, svc, sc)
}

// isSubclusterIdle will run a query to see the number of connections
// that are active for a given subcluster.  It returns a requeue error if there
// are active connections still.
func (o *OnlineImageChangeReconciler) isSubclusterIdle(ctx context.Context, scName string) (ctrl.Result, error) {
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
	// will rely on the exponential backoff algorithm that is in implemented by
	// the controller-runtime: start at 5ms, doubles until it gets to 16minutes.
	return ctrl.Result{Requeue: o.doesScHaveActiveConnections(stdout)}, nil
}

// doesScHaveActiveConnections will parse the output from vsql to see if there
// are any active connections.  Returns true if there is at least one
// connection.
func (o *OnlineImageChangeReconciler) doesScHaveActiveConnections(stdout string) bool {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res != "0"
}
