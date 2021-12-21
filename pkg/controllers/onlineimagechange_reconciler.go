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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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
		// Handle restart of the primary subclusters
		o.restartPrimaries,
		// Handle restart of secondary subclusters
		o.restartSecondaries,
		// Will cleanup the transient subcluster now that the primaries are back up.
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

// restartPrimaries will handle the image change on all of the primaries.
func (o *OnlineImageChangeReconciler) restartPrimaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of primaries")
	stss, err := o.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}

	// We bring all primaries offline before we start to bring any of them back up.
	primaries := []*appsv1.StatefulSet{}

	for i := range stss.Items {
		sts := &stss.Items[i]
		matches := false
		if matches, err = o.isMatchingSubclusterType(sts, vapi.PrimarySubclusterType); err != nil {
			return ctrl.Result{}, err
		} else if !matches {
			continue
		}
		primaries = append(primaries, sts)

		err = o.takeSubclusterOffline(ctx, sts)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	for i := range primaries {
		res, err := o.bringSubclusterOnline(ctx, primaries[i])
		if res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// restartSecondaries will restart all of the secondaries, temporarily
// rerouting traffic to the transient while it does the restart.
func (o *OnlineImageChangeReconciler) restartSecondaries(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting the handling of secondaries")

	stss, err := o.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	// We do each subcluster at a time for secondary.  We can do this
	// differently than primaries because the secondaries aren't needed to form
	// the cluster, so it can be done in a piece meal fashion.
	for i := range stss.Items {
		sts := &stss.Items[i]
		if matches, err := o.isMatchingSubclusterType(sts, vapi.SecondarySubclusterType); err != nil {
			return ctrl.Result{}, err
		} else if !matches {
			continue
		}

		o.Log.Info("Processing secondary", "name", sts.ObjectMeta.Name)

		err := o.takeSubclusterOffline(ctx, sts)
		if err != nil {
			return ctrl.Result{}, err
		}
		res, err := o.bringSubclusterOnline(ctx, sts)
		if res.Requeue || err != nil {
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

// takeSubclusterOffline will take bring down a subcluster if it running the old
// image.  It will reroute client traffic to the transient subcluster so that
// access stays online during the image change.
func (o *OnlineImageChangeReconciler) takeSubclusterOffline(ctx context.Context, sts *appsv1.StatefulSet) error {
	var err error
	scName := sts.Labels[SubclusterNameLabel]
	img := sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image

	if img != o.Vdb.Spec.Image {
		o.Log.Info("starting client traffic routing of secondary to transient", "name", scName)
		err = o.routeClientTraffic(ctx, scName, true)
		if err != nil {
			return err
		}
	}

	stsChanged, err := o.Manager.updateImageInStatefulSet(ctx, sts)
	if err != nil {
		return err
	}
	if stsChanged {
		o.PFacts.Invalidate()
	}

	podsDeleted, err := o.Manager.deletePodsRunningOldImage(ctx, scName)
	if err != nil {
		return err
	}
	if podsDeleted > 0 {
		o.PFacts.Invalidate()
	}
	return nil
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

	if err := o.removeTransientFromVdb(ctx); err != nil {
		return ctrl.Result{}, err
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
	return !o.Vdb.RequiresTransientSubcluster() || (len(o.PrimaryImages) == 1 && o.PrimaryImages[0] == o.Vdb.Spec.Image)
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
			transientSc := buildTransientSubcluster(o.Vdb, sc, "")
			svc.Spec.Selector = makeSvcSelectorLabelsForSubclusterNameRouting(o.Vdb, transientSc)
		}
	} else {
		svc.Spec.Selector = makeSvcSelectorLabelsForServiceNameRouting(o.Vdb, sc)
	}
	return objRec.reconcileExtSvc(ctx, svc, sc)
}
