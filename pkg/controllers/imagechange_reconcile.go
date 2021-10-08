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

// ImageChangeReconciler will handle the process when the vertica image changes
type ImageChangeReconciler struct {
	VRec                  *VerticaDBReconciler
	Log                   logr.Logger
	Vdb                   *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner               cmds.PodRunner
	PFacts                *PodFacts
	ContinuingImageChange bool // true if UpdateInProgress was already set upon entry
	Finder                SubclusterFinder
}

// MakeImageChangeReconciler will build an ImageChangeReconciler object
func MakeImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &ImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder: MakeSubclusterFinder(vdbrecon.Client, vdb),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (u *ImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if u.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	if err := u.PFacts.Collect(ctx, u.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if ok, err := u.isImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		u.startImageChange,
		// Do a clean shutdown of the cluster
		u.stopCluster,
		// Set the new image in the statefulset objects.
		u.updateImageInStatefulSets,
		// Delete pods that have the old image.
		u.deletePods,
		// Check for the pods to be created by the sts controller with the new image
		u.checkForNewPods,
		// Start up vertica in each pod.
		u.restartCluster,
		// Cleanup up the condition and event recording for a completed image change
		u.finishImageChange,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// startImageChange handles condition status and event recording for start of an image change
func (u *ImageChangeReconciler) startImageChange(ctx context.Context) (ctrl.Result, error) {
	u.Log.Info("Starting image change for reconciliation iteration", "ContinuingImageChange", u.ContinuingImageChange)
	if err := u.toggleImageChangeInProgress(ctx, corev1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message the first time we begin an image change.
	if !u.ContinuingImageChange {
		u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.ImageChangeStart,
			"Vertica server image change has started.  New image is '%s'", u.Vdb.Spec.Image)
	}
	return ctrl.Result{}, nil
}

// finishImageChange handles condition status and event recording for the end of an image change
func (u *ImageChangeReconciler) finishImageChange(ctx context.Context) (ctrl.Result, error) {
	if err := u.toggleImageChangeInProgress(ctx, corev1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.ImageChangeSucceeded,
		"Vertica server image change has completed successfully")

	return ctrl.Result{}, nil
}

// isImageChangeNeeded returns true if we are in the middle of an image change or we need to start one
func (u *ImageChangeReconciler) isImageChangeNeeded(ctx context.Context) (bool, error) {
	// We first check if the status condition indicates the image change is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[vapi.ImageChangeInProgress]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", vapi.ImageChangeInProgress)
	}
	if inx < len(u.Vdb.Status.Conditions) && u.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		// Set a flag to indicate that we are continuing an image change.  This silences the ImageChangeStarted event.
		u.ContinuingImageChange = true
		return true, nil
	}

	// Next check if an image change is needed based on the image being different
	// between the Vdb and any of the statefulset's.
	stss, err := u.Finder.FindStatefulSets(ctx, FindInVdb)
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

// toggleImageChangeInProgress is a helper for updating the ImageChangeInProgress condition
func (u *ImageChangeReconciler) toggleImageChangeInProgress(ctx context.Context, newVal corev1.ConditionStatus) error {
	return status.UpdateCondition(ctx, u.VRec.Client, u.Vdb,
		vapi.VerticaDBCondition{Type: vapi.ImageChangeInProgress, Status: newVal},
	)
}

// stopCluster will shutdown the entire cluster using 'admintools -t stop_db'
func (u *ImageChangeReconciler) stopCluster(ctx context.Context) (ctrl.Result, error) {
	pf, found := u.PFacts.findRunningPod()
	if !found {
		u.Log.Info("No pods running so skipping vertica shutdown")
		// No running pod.  This isn't an error, it just means no vertica is
		// running so nothing to shut down.
		return ctrl.Result{}, nil
	}
	if u.PFacts.getUpNodeCount() == 0 {
		u.Log.Info("No vertica process running so nothing to shutdown")
		// No pods have vertica so we can avoid stop_db call
		return ctrl.Result{}, nil
	}

	// Check the running pods.  It is possible that we may have already
	// restarted the cluster with the new image, in which case we don't want to
	// stop the cluster.  As soon as we find a pod that has the old image and is
	// running vertica, then we know we can proceed with the shutdown.
	if ok, err := u.anyPodsRunningWithOldImage(ctx); !ok || err != nil {
		if !ok {
			u.Log.Info("No vertica process running with the old image version")
		}
		return ctrl.Result{}, err
	}

	start := time.Now()
	u.VRec.EVRec.Event(u.Vdb, corev1.EventTypeNormal, events.ClusterShutdownStarted,
		"Calling 'admintools -t stop_db'")

	_, _, err := u.PRunner.ExecAdmintools(ctx, pf.name, names.ServerContainer,
		"-t", "stop_db", "-F", "-d", u.Vdb.Spec.DBName)
	if err != nil {
		u.VRec.EVRec.Event(u.Vdb, corev1.EventTypeWarning, events.ClusterShutdownFailed,
			"Failed to shutdown the cluster")
		return ctrl.Result{}, err
	}

	u.VRec.EVRec.Eventf(u.Vdb, corev1.EventTypeNormal, events.ClusterShutdownSucceeded,
		"Successfully called 'admintools -t stop_db' and it took %s", time.Since(start))
	return ctrl.Result{}, nil
}

// updateImageInStatefulSets will update the statefulsets to have the new image.
// This depends on the statefulsets having the UpdateStrategy of OnDelete.
// Since there will be processing after to delete the pods so that they come up
// with the new image.
func (u *ImageChangeReconciler) updateImageInStatefulSets(ctx context.Context) (ctrl.Result, error) {
	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	stss, err := u.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range stss.Items {
		sts := &stss.Items[i]
		// Skip the statefulset if it already has the proper image.
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			u.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
			sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image = u.Vdb.Spec.Image
			err = u.VRec.Client.Update(ctx, sts)
			if err != nil {
				return ctrl.Result{}, err
			}
			u.PFacts.Invalidate()
		}
	}
	return ctrl.Result{}, nil
}

// deletePods will delete pods that are running the old image.  The assumption
// is that the sts has already had its image updated and the UpdateStrategy for
// the sts is OnDelete.  Deleting the pods ensures they get rescheduled with the
// new image.
func (u *ImageChangeReconciler) deletePods(ctx context.Context) (ctrl.Result, error) {
	// We use FindExisting for the finder because we only want to work with pods
	// that already exist.  This is necessary in case the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	pods, err := u.Finder.FindPods(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		// Skip the pod if it already has the proper image.
		if pod.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			u.Log.Info("Deleting pod that had old image", "name", pod.ObjectMeta.Name)
			err = u.VRec.Client.Delete(ctx, pod)
			if err != nil {
				return ctrl.Result{}, err
			}
			u.PFacts.Invalidate()
		}
	}
	return ctrl.Result{}, nil
}

// checkForNewPods will check to ensure at least one pod exists with the new image.
// This is necessary before proceeding to the restart phase.  We need at least
// one pod to exist with the new image.  Failure to do this will cause the
// restart process to exit successfully with no restart done.  A restart can
// only occur if there is at least one pod that exists.
func (u *ImageChangeReconciler) checkForNewPods(ctx context.Context) (ctrl.Result, error) {
	foundPodWithNewImage := false
	pods, err := u.Finder.FindPods(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.Containers[names.ServerContainerIndex].Image == u.Vdb.Spec.Image {
			foundPodWithNewImage = true
			break
		}
	}
	if !foundPodWithNewImage {
		u.Log.Info("Requeue to wait until at least one pod exists with the new image")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// restartCluster will start up vertica.  This is called after the statefulset's have
// been recreated.  Once the cluster is back up, then the image change is considered complete.
func (u *ImageChangeReconciler) restartCluster(ctx context.Context) (ctrl.Result, error) {
	u.Log.Info("Starting restart phase of image change for this reconcile iteration")
	// The restart reconciler is called after this reconciler.  But we call the
	// restart reconciler here so that we restart while the status condition is set.
	r := MakeRestartReconciler(u.VRec, u.Log, u.Vdb, u.PRunner, u.PFacts)
	return r.Reconcile(ctx, &ctrl.Request{})
}

// anyPodsRunningWithOldImage will check if any upNode pods are running with the old image.
func (u *ImageChangeReconciler) anyPodsRunningWithOldImage(ctx context.Context) (bool, error) {
	for pn, pf := range u.PFacts.Detail {
		if !pf.upNode {
			continue
		}

		pod := &corev1.Pod{}
		err := u.VRec.Client.Get(ctx, pn, pod)
		if err != nil && !errors.IsNotFound(err) {
			return false, fmt.Errorf("error getting pod info for '%s'", pn)
		}
		if errors.IsNotFound(err) {
			continue
		}

		if pod.Spec.Containers[names.ServerContainerIndex].Image != u.Vdb.Spec.Image {
			return true, nil
		}
	}
	return false, nil
}
