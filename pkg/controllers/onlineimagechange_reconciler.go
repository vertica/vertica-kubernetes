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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
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
		// Setup a transient subcluster to accept traffic when other subclusters
		// are down
		o.createTransientSts,
		o.installTransientNodes,
		o.addTransientSubcluster,
		o.addTransientNodes,
		// Reroute all traffic from primary subclusters to the transient
		o.rerouteClientTrafficToTransient,
		// Drain all connections from the primary subcluster.  This waits for
		// connections that were established before traffic was routed to the
		// transient.
		o.drainPrimaries,
		// Change the image in each of the primary subclusters.
		o.changeImageInPrimaries,
		// Restart the pods of the primary subclusters.
		o.restartPrimaries,
		// Reroute all traffic from transient subcluster back to the primaries
		o.rerouteClientTrafficToPrimaries,
		// Drain all connections from the transient subcluster to prepare it
		// for being removed.
		o.drainTransient,
		// Will cleanup the transient subcluster now that the primaries are back up.
		o.removeTransientSubclusters,
		o.uninstallTransientNodes,
		o.deleteTransientSts,
		// With the primaries back up, we can do a "rolling upgrade" style of
		// update for the secondary subclusters.
		o.startRollingUpgradeOfSecondarySubclusters,
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

// createTransientSts this will create a secondary subcluster to accept
// traffic from subclusters when they are down.  This subcluster is called
// the transient and only exist for the life of the image change.
func (o *OnlineImageChangeReconciler) createTransientSts(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	if err := o.addTransientToVdb(ctx); err != nil {
		return ctrl.Result{}, err
	}
	o.Log.Info("Adding transient", "num subclusters", len(o.Vdb.Spec.Subclusters))

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
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
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// addTransientNodes will ensure nodes on the transient have been added to the
// cluster.
func (o *OnlineImageChangeReconciler) addTransientNodes(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	actor := MakeDBAddNodeReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// rerouteClientTrafficToTransient will update the service objects for each of the
// primary subclusters so that they are routed to the transient subcluster.
func (o *OnlineImageChangeReconciler) rerouteClientTrafficToTransient(ctx context.Context) (ctrl.Result, error) {
	if o.skipTransientSetup() {
		return ctrl.Result{}, nil
	}

	o.Log.Info("starting client traffic routing to transient")
	err := o.routeClientTraffic(ctx, func(sc *vapi.Subcluster) bool { return sc.IsTransient })
	return ctrl.Result{}, err
}

// drainPrimaries will only succeed if the primary subclusters are already down
// or have no active connections.  All traffic to the primaries get routed to
// the transient subcluster, so this step waits for any connection that were
// established before the transient was created.
func (o *OnlineImageChangeReconciler) drainPrimaries(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// changeImageInPrimaries will update the statefulset of each of the primary
// subcluster's with the new image.  It will also force the cluster in read-only
// mode as all of the pods in the primary will be rescheduled with the new
// image.
func (o *OnlineImageChangeReconciler) changeImageInPrimaries(ctx context.Context) (ctrl.Result, error) {
	numStsChanged, res, err := o.Manager.updateImageInStatefulSets(ctx, true, false)
	if numStsChanged > 0 {
		o.Log.Info("changed image in statefulsets", "num", numStsChanged)
		o.PFacts.Invalidate()
	}
	return res, err
}

// restartPrimaries will restart all of the pods in the primary subclusters.
func (o *OnlineImageChangeReconciler) restartPrimaries(ctx context.Context) (ctrl.Result, error) {
	numPodsDeleted, res, err := o.Manager.deletePodsRunningOldImage(ctx, false)
	if res.Requeue || err != nil {
		return res, err
	}
	if numPodsDeleted > 0 {
		o.Log.Info("deleted pods running old image", "num", numPodsDeleted)
		o.PFacts.Invalidate()
	}

	const DoNotRestartReadOnly = false
	actor := MakeRestartReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, DoNotRestartReadOnly)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// rerouteClientTrafficToPrimaries will update the service objects of the primary
// subclusters so that traffic is not routed to the transient anymore but back
// to the primary subclusters.
func (o *OnlineImageChangeReconciler) rerouteClientTrafficToPrimaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("starting client traffic routing to primary")
	err := o.routeClientTraffic(ctx, func(sc *vapi.Subcluster) bool { return sc.IsPrimary })
	return ctrl.Result{}, err
}

// drainTransient will wait for all active connections in the transient subcluster
// to leave.  This is preparation for eventual removal of the transient subcluster.
func (o *OnlineImageChangeReconciler) drainTransient(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// removeTransientSubclusters will drive subcluster removal of the transient subcluster
func (o *OnlineImageChangeReconciler) removeTransientSubclusters(ctx context.Context) (ctrl.Result, error) {
	actor := MakeDBRemoveSubclusterReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// uninstallTransientNodes will drive uninstall logic for any transient nodes.
func (o *OnlineImageChangeReconciler) uninstallTransientNodes(ctx context.Context) (ctrl.Result, error) {
	actor := MakeUninstallReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// deleteTransientSts will delete the transient subcluster that was created for the image change.
func (o *OnlineImageChangeReconciler) deleteTransientSts(ctx context.Context) (ctrl.Result, error) {
	if err := o.removeTransientFromVdb(ctx); err != nil {
		return ctrl.Result{}, err
	}

	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	o.traceActorReconcile(actor)
	return actor.Reconcile(ctx, &ctrl.Request{})
}

// startRollingUpgradeOfSecondarySubclusters will update the image of each of
// the secondary subclusters.  The update policy will be rolling upgrade.  This
// gives control of restarting each pod back to k8s.  This can be done because
// secondary subclusters don't participate in cluster quorum.
func (o *OnlineImageChangeReconciler) startRollingUpgradeOfSecondarySubclusters(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
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
	return len(o.PrimaryImages) == 1 && o.PrimaryImages[0] == o.Vdb.Spec.Image
}

// addTransientToVdb will create a transient subcluster. The transient is added
// to the Vdb struct inplace.
func (o *OnlineImageChangeReconciler) addTransientToVdb(ctx context.Context) error {
	oldImage, ok := o.fetchOldImage()
	if !ok {
		return fmt.Errorf("could not determine the old image name.  "+
			"Only available image is %s", o.Vdb.Spec.Image)
	}

	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := types.NamespacedName{Namespace: o.Vdb.Namespace, Name: o.Vdb.Name}
		if err := o.VRec.Client.Get(ctx, nm, o.Vdb); err != nil {
			return err
		}

		// Figure out if a transient needs to be added
		scMap := o.Vdb.GenSubclusterMap()
		for i := range o.Vdb.Spec.Subclusters {
			sc := &o.Vdb.Spec.Subclusters[i]
			if sc.IsPrimary {
				transient := buildTransientSubcluster(o.Vdb, sc, oldImage)
				_, ok := scMap[transient.Name]
				if !ok {
					if err := o.Manager.setImageChangeStatus(ctx, "Creating transient subcluster"); err != nil {
						return err
					}
					o.Vdb.Spec.Subclusters = append(o.Vdb.Spec.Subclusters, *transient)
					return o.VRec.Client.Update(ctx, o.Vdb)
				}
			}
		}
		return nil
	})
}

// removeTransientFromVdb will delete any transientsubcluster that exists.  The
// transient will be removed from the Vdb struct inplace.
func (o *OnlineImageChangeReconciler) removeTransientFromVdb(ctx context.Context) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := types.NamespacedName{Namespace: o.Vdb.Namespace, Name: o.Vdb.Name}
		if err := o.VRec.Client.Get(ctx, nm, o.Vdb); err != nil {
			return err
		}

		scToKeep := []vapi.Subcluster{}
		for i := range o.Vdb.Spec.Subclusters {
			sc := &o.Vdb.Spec.Subclusters[i]
			if !sc.IsTransient {
				scToKeep = append(scToKeep, *sc)
			}
		}

		if len(scToKeep) != len(o.Vdb.Spec.Subclusters) {
			o.Vdb.Spec.Subclusters = scToKeep
			return o.VRec.Client.Update(ctx, o.Vdb)
		}
		return nil
	})
}

func (o *OnlineImageChangeReconciler) traceActorReconcile(actor ReconcileActor) {
	o.Log.Info("starting actor for online image change", "name", fmt.Sprintf("%T", actor))
}

// routeClientTraffic will update service objects to route to either the primary
// or standby.  The subcluster picked is determined by the scCheckFunc the
// caller provides.  If it returns true for a given subcluster, traffic will be
// routed to that.
func (o *OnlineImageChangeReconciler) routeClientTraffic(ctx context.Context, scCheckFunc func(sc *vapi.Subcluster) bool) error {
	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	objRec := actor.(*ObjReconciler)

	for i := range o.Vdb.Spec.Subclusters {
		sc := &o.Vdb.Spec.Subclusters[i]
		if scCheckFunc(sc) {
			if err := objRec.reconcileExtSvc(ctx, sc); err != nil {
				return err
			}
		}
	}
	return nil
}
