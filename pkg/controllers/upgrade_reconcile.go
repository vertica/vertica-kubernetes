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
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeReconciler will update the status field of the vdb.
type UpgradeReconciler struct {
	VRec              *VerticaDBReconciler
	Log               logr.Logger
	Vdb               *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner           cmds.PodRunner
	PFacts            *PodFacts
	ContinuingUpgrade bool // true if UpdateInProgress was already set upon entry
}

// MakeUpgradeReconciler will build an UpgradeReconciler object
func MakeUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &UpgradeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will update the status of the Vdb based on the pod facts
func (u *UpgradeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := u.PFacts.Collect(ctx, u.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if ok, err := u.isUpgradeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	// SPILLY - should we call status reconciler after a few so that we get up to date status?

	// Functions to perform upgrade processing.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an upgrade by setting condition and event recording
		u.startUpgrade,
		// Do a clean shutdown of the cluster
		u.stopCluster,
		// Delete the sts objects.  We don't rely on k8s rolling upgrade.
		// Everything must be destroyed then regenerated.
		u.deleteStatefulSets,
		// Create the sts object with the new image name.
		u.recreateStatefulSets,
		// Start up vertica in each pod.
		u.restartCluster,
		// Cleanup up the condition and event recording for a completed upgrade
		u.finishUpgrade,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// startUpgrade handles condition status and event recording for start of upgrade
func (u *UpgradeReconciler) startUpgrade(ctx context.Context) (ctrl.Result, error) {
	if err := u.toggleUpgradeInProgress(ctx, corev1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message the first time we begin an upgrade.
	if !u.ContinuingUpgrade {
		u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.UpgradeStart,
			"Vertica server upgrade has started.  Upgrading to image '%s'", u.Vdb.Spec.Image)
	}
	return ctrl.Result{}, nil
}

// finishUpgrade handles condition status and event recording for the end of an upgrade
func (u *UpgradeReconciler) finishUpgrade(ctx context.Context) (ctrl.Result, error) {
	if err := u.toggleUpgradeInProgress(ctx, corev1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.UpgradeSucceeded,
		"Vertica server upgrade has completed successfully")

	return ctrl.Result{}, nil
}

// isUpgradeNeeded returns true if we are in the middle of an upgrade or we need to start one
func (u *UpgradeReconciler) isUpgradeNeeded(ctx context.Context) (bool, error) {
	// We first check if the status condition indicates the upgrade is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[vapi.UpgradeInProgress]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", vapi.UpgradeInProgress)
	}
	if inx < len(u.Vdb.Status.Conditions) && u.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		// Set a flag to indicate that we are continuing an upgrade.  This silences the UpgradeStarted event.
		u.ContinuingUpgrade = true
		return true, nil
	}

	// Next check if an upgrade is needed based on the image being different
	// between the Vdb and any of the statefulset's.
	finder := MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, FindInVdb)
	if err != nil {
		return false, err
	}
	for i := range stss.Items {
		sts := stss.Items[i]
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			return true, nil
		}
	}

	return false, nil
}

// toggleUpgradeInProgress is a helper for updating the UpgradeInProgress condition
func (u *UpgradeReconciler) toggleUpgradeInProgress(ctx context.Context, newVal corev1.ConditionStatus) error {
	return status.UpdateCondition(ctx, u.VRec.Client, u.Vdb,
		vapi.VerticaDBCondition{Type: vapi.UpgradeInProgress, Status: newVal},
	)
}

// stopCluster will shutdown the entire cluster using 'admintools -t stop_db'
func (u *UpgradeReconciler) stopCluster(ctx context.Context) (ctrl.Result, error) {
	pf, found := u.PFacts.findRunningPod()
	if !found {
		// No running pod.  This isn't an error, it just means no vertica is
		// running so nothing to shut down.
		return ctrl.Result{}, nil
	}
	if u.PFacts.getUpNodeCount() == 0 {
		// No pods have vertica so we can avoid stop_db call
		return ctrl.Result{}, nil
	}

	// Check the image of each of the up nodes.  We avoid doing a shutdown if
	// all of the up nodes are already on the new image.
	if ok, err := u.anyPodsRunningWithOldImage(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	start := time.Now()
	u.VRec.EVRec.Event(u.Vdb, corev1.EventTypeNormal, events.ClusterShutdownStarted,
		"Calling 'admintools -t stop_db'")

	_, _, err := u.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer,
		"/opt/vertica/bin/admintools", "-t", "stop_db", "-F", "-d", u.Vdb.Spec.DBName)
	if err != nil {
		u.VRec.EVRec.Event(u.Vdb, corev1.EventTypeWarning, events.ClusterShutdownFailed,
			"Failed to shutdown the cluster")
		return ctrl.Result{}, err
	}

	u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.ClusterShutdownSucceeded,
		"Successfully called 'admintools -t stop_db' and it took %s", time.Since(start))
	return ctrl.Result{}, nil
}

// deleteStatefulSets will delete the statefulsets and all of their pods.  The
// purpose is so that all pods get recreated with the new image.  K8s has the
// ability to support rolling upgrade.  However, the vertica server doesn't
// support this.  In fact, all hosts in a cluster must be on the same engine
// level.  So an upgrade incurs some downtime as we need to take the entire
// cluster down.   That is what this function is doing, it is delete the
// statefulset so they can be regenerated with the new image.
func (u *UpgradeReconciler) deleteStatefulSets(ctx context.Context) (ctrl.Result, error) {
	finder := MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, FindInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range stss.Items {
		sts := stss.Items[i]
		// Skip the statefulset if it already has the proper image.
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			err = u.VRec.Client.Delete(ctx, &stss.Items[i])
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}
	return ctrl.Result{}, nil
}

// recreateStatefulSets will regenerate the statefulset and pods.  The new
// regenerated sts will have the new image.
func (u *UpgradeReconciler) recreateStatefulSets(ctx context.Context) (ctrl.Result, error) {
	actor := MakeObjReconciler(u.VRec.Client, u.VRec.Scheme, u.Log, u.Vdb, u.PFacts)
	objr := actor.(*ObjReconciler)
	objr.PatchImageAllowed = true

	// We are only going to call a subset of the ObjReconciler functionality.
	// We don't want to reconcile other changes like svc objects.  We just want
	// to recreate any statefulset objects.
	for i := range u.Vdb.Spec.Subclusters {
		updated, err := objr.reconcileSts(ctx, &u.Vdb.Spec.Subclusters[i])
		if err != nil {
			return ctrl.Result{}, err
		}
		// Invalidate the pfacts since objects were recreated
		if updated {
			u.PFacts.Invalidate()
		}
	}
	return ctrl.Result{}, nil
}

// restartCluster will start up vertica.  This is called after the statefulset's have
// been recreated.  Once the cluster is back up, then the upgrade is considered complete.
func (u *UpgradeReconciler) restartCluster(ctx context.Context) (ctrl.Result, error) {
	// The restart reconciler is called after this reconciler.  But we call the
	// restart reconciler here so that we restart while the status condition is set.
	actor := MakeRestartReconciler(u.VRec, u.Log, u.Vdb, u.PRunner, u.PFacts)
	resr := actor.(*RestartReconciler)
	// Since we just regenerated the sts, chances are that the initial attempt to
	// restart will not find any pods running.  We want to override the default
	// behavior and requeue the iteration if that is the case so that restart
	// for upgrade is controller entirely within this reconciler.
	resr.RequeueIfRunningAndInstalledIsZero = true
	return resr.Reconcile(ctx, &ctrl.Request{})
}

// anyPodsRunningWithOldImage will check if any upNode pods are running with the old image.
func (u *UpgradeReconciler) anyPodsRunningWithOldImage(ctx context.Context) (bool, error) {
	for pn, pf := range u.PFacts.Detail {
		if !pf.upNode {
			continue
		}

		pod := &corev1.Pod{}
		err := u.VRec.Client.Get(ctx, pn, pod)
		if err != nil && !errors.IsNotFound(err) {
			return false, fmt.Errorf("error getting pod info for '%s'", pn)
		}

		if pod.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			return true, nil
		}
	}
	return false, nil
}
