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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OfflineImageChangeReconciler will handle the process when the vertica image changes
type OfflineImageChangeReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	Finder  SubclusterFinder
	Manager ImageChangeManager
}

const (
	ClusterShutdownOfflineMsgIndex = iota
	ReschedulePodsOfflineMsgIndex
	ClusterRestartOfflineMsgIndex
)

var OfflineImageChangeStatusMsgs = []string{
	"Shutting down cluster",
	"Rescheduling pods with new image",
	"Restarting cluster with new image",
}

// MakeOfflineImageChangeReconciler will build an OfflineImageChangeReconciler object
func MakeOfflineImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &OfflineImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder:  MakeSubclusterFinder(vdbrecon.Client, vdb),
		Manager: *MakeImageChangeManager(vdbrecon, log, vdb, vapi.OfflineImageChangeInProgress, offlineImageChangeAllowed),
	}
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OfflineImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Manager.IsImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		o.Manager.startImageChange,
		// Do a clean shutdown of the cluster
		o.postStoppingClusterMsg,
		o.stopCluster,
		// Set the new image in the statefulset objects.
		o.postReschedulePodsMsg,
		o.updateImageInStatefulSets,
		// Delete pods that have the old image.
		o.deletePods,
		// Check for the pods to be created by the sts controller with the new image
		o.checkForNewPods,
		// Start up vertica in each pod.
		o.postRestartingClusterMsg,
		o.restartCluster,
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

// postStoppingClusterMsg will update the status message to indicate a cluster
// shutdown has commenced.
func (o *OfflineImageChangeReconciler) postStoppingClusterMsg(ctx context.Context) (ctrl.Result, error) {
	return o.postNextStatusMsg(ctx, ClusterShutdownOfflineMsgIndex)
}

// stopCluster will shutdown the entire cluster using 'admintools -t stop_db'
func (o *OfflineImageChangeReconciler) stopCluster(ctx context.Context) (ctrl.Result, error) {
	pf, found := o.PFacts.findRunningPod()
	if !found {
		o.Log.Info("No pods running so skipping vertica shutdown")
		// No running pod.  This isn't an error, it just means no vertica is
		// running so nothing to shut down.
		return ctrl.Result{}, nil
	}
	if o.PFacts.getUpNodeCount() == 0 {
		o.Log.Info("No vertica process running so nothing to shutdown")
		// No pods have vertica so we can avoid stop_db call
		return ctrl.Result{}, nil
	}

	// Check the running pods.  It is possible that we may have already
	// restarted the cluster with the new image, in which case we don't want to
	// stop the cluster.  As soon as we find a pod that has the old image and is
	// running vertica, then we know we can proceed with the shutdown.
	if ok, err := o.anyPodsRunningWithOldImage(ctx); !ok || err != nil {
		if !ok {
			o.Log.Info("No vertica process running with the old image version")
		}
		return ctrl.Result{}, err
	}

	start := time.Now()
	o.VRec.EVRec.Event(o.Vdb, corev1.EventTypeNormal, events.ClusterShutdownStarted,
		"Calling 'admintools -t stop_db'")

	_, _, err := o.PRunner.ExecAdmintools(ctx, pf.name, names.ServerContainer,
		"-t", "stop_db", "-F", "-d", o.Vdb.Spec.DBName)
	if err != nil {
		o.VRec.EVRec.Event(o.Vdb, corev1.EventTypeWarning, events.ClusterShutdownFailed,
			"Failed to shutdown the cluster")
		return ctrl.Result{}, err
	}

	o.VRec.EVRec.Eventf(o.Vdb, corev1.EventTypeNormal, events.ClusterShutdownSucceeded,
		"Successfully called 'admintools -t stop_db' and it took %s", time.Since(start))
	return ctrl.Result{}, nil
}

// postReschedulePodsMsg will update the status message to indicate new pods
// have been rescheduled with the new image.
func (o *OfflineImageChangeReconciler) postReschedulePodsMsg(ctx context.Context) (ctrl.Result, error) {
	return o.postNextStatusMsg(ctx, ReschedulePodsOfflineMsgIndex)
}

// updateImageInStatefulSets will update the statefulsets to have the new image.
// This depends on the statefulsets having the UpdateStrategy of OnDelete.
// Since there will be processing after to delete the pods so that they come up
// with the new image.
func (o *OfflineImageChangeReconciler) updateImageInStatefulSets(ctx context.Context) (ctrl.Result, error) {
	numStsChanged, res, err := o.Manager.updateImageInStatefulSets(ctx)
	if numStsChanged > 0 {
		o.PFacts.Invalidate()
	}
	return res, err
}

// deletePods will delete pods that are running the old image.  The assumption
// is that the sts has already had its image updated and the UpdateStrategy for
// the sts is OnDelete.  Deleting the pods ensures they get rescheduled with the
// new image.
func (o *OfflineImageChangeReconciler) deletePods(ctx context.Context) (ctrl.Result, error) {
	numPodsDeleted, err := o.Manager.deletePodsRunningOldImage(ctx, "")
	if numPodsDeleted > 0 {
		o.PFacts.Invalidate()
	}
	return ctrl.Result{}, err
}

// checkForNewPods will check to ensure at least one pod exists with the new image.
// This is necessary before proceeding to the restart phase.  We need at least
// one pod to exist with the new image.  Failure to do this will cause the
// restart process to exit successfully with no restart done.  A restart can
// only occur if there is at least one pod that exists.
func (o *OfflineImageChangeReconciler) checkForNewPods(ctx context.Context) (ctrl.Result, error) {
	foundPodWithNewImage := false
	pods, err := o.Finder.FindPods(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if pod.Spec.Containers[names.ServerContainerIndex].Image == o.Vdb.Spec.Image {
			foundPodWithNewImage = true
			break
		}
	}
	if !foundPodWithNewImage {
		o.Log.Info("Requeue to wait until at least one pod exists with the new image")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// postRestartingClusterMsg will update the status message to indicate the
// cluster is being restarted
func (o *OfflineImageChangeReconciler) postRestartingClusterMsg(ctx context.Context) (ctrl.Result, error) {
	return o.postNextStatusMsg(ctx, ClusterRestartOfflineMsgIndex)
}

// restartCluster will start up vertica.  This is called after the statefulset's have
// been recreated.  Once the cluster is back up, then the image change is considered complete.
func (o *OfflineImageChangeReconciler) restartCluster(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting restart phase of image change for this reconcile iteration")

	// The restart reconciler is called after this reconciler.  But we call the
	// restart reconciler here so that we restart while the status condition is set.
	r := MakeRestartReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts, true)
	return r.Reconcile(ctx, &ctrl.Request{})
}

// anyPodsRunningWithOldImage will check if any upNode pods are running with the old image.
func (o *OfflineImageChangeReconciler) anyPodsRunningWithOldImage(ctx context.Context) (bool, error) {
	for pn, pf := range o.PFacts.Detail {
		if !pf.upNode {
			continue
		}

		pod := &corev1.Pod{}
		err := o.VRec.Client.Get(ctx, pn, pod)
		if err != nil && !errors.IsNotFound(err) {
			return false, fmt.Errorf("error getting pod info for '%s'", pn)
		}
		if errors.IsNotFound(err) {
			continue
		}

		if pod.Spec.Containers[names.ServerContainerIndex].Image != o.Vdb.Spec.Image {
			return true, nil
		}
	}
	return false, nil
}

// postNextStatusMsg will set the next status message for an online image change
// according to msgIndex
func (o *OfflineImageChangeReconciler) postNextStatusMsg(ctx context.Context, msgIndex int) (ctrl.Result, error) {
	return ctrl.Result{}, o.Manager.postNextStatusMsg(ctx, OfflineImageChangeStatusMsgs, msgIndex)
}
