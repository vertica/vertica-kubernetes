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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/status"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// UpgradeReconciler will update the status field of the vdb.
type UpgradeReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
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

	if ok, err := u.upgradeIsNotNeeded(ctx); ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := u.updateCondition(ctx, corev1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform upgrade processing.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Do a clean shutdown of the cluster
		u.stopCluster,
		// Delete the sts objects.  We don't rely on k8s rolling upgrade.
		// Everything must be destroyed then regenerated.
		u.deleteStatefulSets,
		// Create the sts object with the new image name.
		u.recreateStatefulSets,
		// Start up vertica in each pod.
		u.restartCluster,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	if err := u.updateCondition(ctx, corev1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// upgradeIsNotNeeded returns true if an upgrade isn't needed.
func (u *UpgradeReconciler) upgradeIsNotNeeded(ctx context.Context) (bool, error) {
	// We first check if the status condition indicates the upgrade is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[vapi.UpgradeInProgress]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", vapi.UpgradeInProgress)
	}

	if inx < len(u.Vdb.Status.Conditions) && u.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		return false, nil
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
			return false, nil
		}
	}

	return true, nil
}

// updateCondition is a helper for updating the UpgradeInProgress condition
func (u *UpgradeReconciler) updateCondition(ctx context.Context, newVal corev1.ConditionStatus) error {
	return status.UpdateCondition(ctx, u.VRec.Client, u.Vdb,
		vapi.VerticaDBCondition{Type: vapi.UpgradeInProgress, Status: newVal},
	)
}

// SPILLY - add events
// SPILLY - update language about the purpose of autoRestartVertica

// stopCluster will shutdown the entire cluster using 'admintools -t stop_db'
func (u *UpgradeReconciler) stopCluster(ctx context.Context) (ctrl.Result, error) {
	// SPILLY - you need to check if the database exists.  We skip the stop if the database doesn't exist.
	// SPILLY - we will want to shut down only if (a) a pod is running and (b) it's not running the proper image.
	pf, found := u.PFacts.findRunningPod()
	if !found {
		// No running pod.  This isn't an error, it just means no vertica is
		// running so nothing to shut down.
		return ctrl.Result{}, nil
	}
	_, _, err := u.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer,
		"/opt/vertica/bin/admintools", "-t", "stop_db", "-F", "-d", u.Vdb.Spec.DBName)
	return ctrl.Result{}, err
}

// deleteStatefulSets will delete the statefulsets and all of their pods.  The
// purpose is so that all pods get recreated with the new image.
func (u *UpgradeReconciler) deleteStatefulSets(ctx context.Context) (ctrl.Result, error) {
	// SPILLY - comments
	finder := MakeSubclusterFinder(u.VRec.Client, u.Vdb)
	stss, err := finder.FindStatefulSets(ctx, FindInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range stss.Items {
		err = u.VRec.Client.Delete(ctx, &stss.Items[i])
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// recreateStatefulSets will regenerate the statefulset and pods.  The new
// regenerated sts will have the new image.
func (u *UpgradeReconciler) recreateStatefulSets(ctx context.Context) (ctrl.Result, error) {
	objr := MakeObjReconciler(u.VRec.Client, u.VRec.Scheme, u.Log, u.Vdb, u.PFacts)
	return objr.Reconcile(ctx, &ctrl.Request{})
}

// restartCluster will start up vertica.  This is called after the statefulset's have
// been recreated.  Once the cluster is back up, then the upgrade is considered complete.
func (u *UpgradeReconciler) restartCluster(ctx context.Context) (ctrl.Result, error) {
	// The restart reconciler is called after this reconciler.  But we call the
	// restart reconciler here so that we restart while the status condition is set.
	resr := MakeRestartReconciler(u.VRec, u.Log, u.Vdb, u.PRunner, u.PFacts)
	return resr.Reconcile(ctx, &ctrl.Request{})
}
