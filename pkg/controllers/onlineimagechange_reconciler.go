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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OnlineImageChangeReconciler will handle the process when the vertica image
// changes.  It does this while keeping the database online.
type OnlineImageChangeReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner     cmds.PodRunner
	PFacts      *PodFacts
	Finder      SubclusterFinder
	Manager     ImageChangeManager
	Subclusters []*SubclusterHandle
}

// MakeOnlineImageChangeReconciler will build an OnlineImageChangeReconciler object
func MakeOnlineImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &OnlineImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder:  MakeSubclusterFinder(vdbrecon.Client, vdb),
		Manager: *MakeImageChangeManager(vdbrecon, log, vdb, onlineImageChangeAllowed),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OnlineImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Manager.IsImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	// SPILLY - we may need to add podfacts collection, something to force collection of pods for standbys

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		o.Manager.startImageChange,
		// Load up state that is used for the subsequent steps
		o.loadSubclusterState,
		// Setup a secondary standby subcluster for each primary
		o.createStandbySubclusters,
		o.installVerticaOnStandbySubclusters,
		o.addNodesOnStandbySubclusters,
		// Reroute all traffic from primary subclusters to their standby's
		o.rerouteClientTrafficToStandby,
		// Drain all connections from the primary subcluster.  This waits for
		// connections that were established before traffic was routed to the
		// standby's.
		o.drainPrimaries,
		// Change the image in each of the primary subclusters.
		o.changeImageInPrimaries,
		// Restart the pods of the primary subclusters.
		o.restartPrimaries,
		// Reroute all traffic from standby subclusters back to the primary
		o.rerouteClientTrafficToPrimary,
		// Drain all connections from the standby subclusters to prepare them
		// for being removed.
		o.drainStandbys,
		// Will delete the standby subclusters now that the primaries are back up.
		o.deleteStandbySubclusters,
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

	o.Subclusters, err = o.Finder.FindSubclusterHandles(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// createStandbySubclusters this will create a secondary subcluster to accept
// traffic from the primaries when they are down.  These subclusters are scalled
// standby and are transient since they only exist for the life of the image
// change.
func (o *OnlineImageChangeReconciler) createStandbySubclusters(ctx context.Context) (ctrl.Result, error) {
	if o.skipStandbySetup() {
		return ctrl.Result{}, nil
	}

	// We create a standby subcluster for each primary
	actor := MakeObjReconciler(o.VRec, o.Log, o.Vdb, o.PFacts)
	objRec := actor.(*ObjReconciler)
	for i := range o.Vdb.Spec.Subclusters {
		if o.Vdb.Spec.Subclusters[i].IsPrimary {
			sch := makeStandbySubclusterHandle(&o.Vdb.Spec.Subclusters[i])
			nm := names.GenStandbyStsName(o.Vdb, &sch.Subcluster)
			if modifiedSts, res, err := objRec.reconcileSts(ctx, nm, sch); res.Requeue || err != nil {
				return res, err
			} else if modifiedSts {
				if err2 := o.Manager.setImageChangeStatus(ctx, "Creating standby secondary subclusters"); err2 != nil {
					return res, err2
				}
			}
		}
	}
	// SPILLY - need to handle install and add node
	return ctrl.Result{}, nil
}

// installVerticaOnStandbySubclusters will ensure we have installed vertica on
// each of the standby nodes.
func (o *OnlineImageChangeReconciler) installVerticaOnStandbySubclusters(ctx context.Context) (ctrl.Result, error) {
	if o.skipStandbySetup() {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// addNodesOnStandbySubclusters will ensure nodes on the standby's have been
// added to the cluster.
func (o *OnlineImageChangeReconciler) addNodesOnStandbySubclusters(ctx context.Context) (ctrl.Result, error) {
	if o.skipStandbySetup() {
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// rerouteClientTrafficToStandby will update the service objects for each of the
// primary subclusters so that they are routed to the standby subclusters.
func (o *OnlineImageChangeReconciler) rerouteClientTrafficToStandby(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// drainPrimaries will only succeed if the primary subclusters are already down
// or have no active connections.  All traffic to the primaries get routed to
// the standby subclusters, so this step waits for any connection that were
// established before the standby's were created.
func (o *OnlineImageChangeReconciler) drainPrimaries(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// changeImageInPrimaries will update the statefulset of each of the primary
// subcluster's with the new image.  It will also force the cluster in read-only
// mode as all of the pods in the primary will be rescheduled with the new
// image.
func (o *OnlineImageChangeReconciler) changeImageInPrimaries(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// restartPrimaries will restart all of the pods in the primary subclusters.
func (o *OnlineImageChangeReconciler) restartPrimaries(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// rerouteClientTrafficToPrimary will update the service objects of the primary
// subclusters so that traffic is not routed to the standby's anymore but back
// to te primary subclusters.
func (o *OnlineImageChangeReconciler) rerouteClientTrafficToPrimary(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// drainStandbys will wait for all active connections in the standby subclusters
// to leave.  This is preparation for eventual removal of the standby
// subclusters.
func (o *OnlineImageChangeReconciler) drainStandbys(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// deleteStandbySubclusters will delete any standby subclusters that were created for the image change.
func (o *OnlineImageChangeReconciler) deleteStandbySubclusters(ctx context.Context) (ctrl.Result, error) {
	for i := range o.Subclusters {
		sc := o.Subclusters[i]
		if sc.IsStandby {
			sts := &appsv1.StatefulSet{}
			err := o.VRec.Client.Get(ctx, names.GenStandbyStsName(o.Vdb, &sc.Subcluster), sts)
			if err != nil {
				if errors.IsNotFound(err) {
					continue
				}
				return ctrl.Result{}, err
			}
			err = o.VRec.Client.Delete(ctx, sts)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// startRollingUpgradeOfSecondarySubclusters will update the image of each of
// the secondary subclusters.  The update policy will be rolling upgrade.  This
// gives control of restarting each pod back to k8s.  This can be done because
// secondary subclusters don't participate in cluster quorum.
func (o *OnlineImageChangeReconciler) startRollingUpgradeOfSecondarySubclusters(ctx context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// allPrimariesHaveNewImage returns true if all of the primary subclusters have the new image
func (o *OnlineImageChangeReconciler) allPrimariesHaveNewImage() bool {
	for i := range o.Subclusters {
		sc := o.Subclusters[i]
		if sc.IsPrimary && sc.Image != o.Vdb.Spec.Image {
			return false
		}
	}
	return true
}

// skipStandbySetup will return true if we can skip creation, install and
// scale-out of the standby subcluster
func (o *OnlineImageChangeReconciler) skipStandbySetup() bool {
	// We can skip this entirely if all of the primary subclusters already have
	// the new image.  This is an indication that we have already created the
	// standbys and done the image change.
	return o.allPrimariesHaveNewImage()
}
