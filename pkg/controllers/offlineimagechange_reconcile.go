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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// OfflineImageChangeReconciler will handle the process when the vertica image changes
type OfflineImageChangeReconciler struct {
	VRec      *VerticaDBReconciler
	Log       logr.Logger
	Vdb       *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner   cmds.PodRunner
	PFacts    *PodFacts
	Finder    SubclusterFinder
	Initiator ImageChangeInitiator
}

// MakeOfflineImageChangeReconciler will build an OfflineImageChangeReconciler object
func MakeOfflineImageChangeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	r := &OfflineImageChangeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		Finder: MakeSubclusterFinder(vdbrecon.Client, vdb),
	}
	r.Initiator = *MakeImageChangeInitiator(vdbrecon, log, vdb, r)
	return r
}

// Reconcile will handle the process of the vertica image changing.  For
// example, this can automate the process for an upgrade.
func (o *OfflineImageChangeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if ok, err := o.Initiator.IsImageChangeNeeded(ctx); !ok || err != nil {
		return ctrl.Result{}, err
	}

	if err := o.PFacts.Collect(ctx, o.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Functions to perform when the image changes.  Order matters.
	funcs := []func(context.Context) (ctrl.Result, error){
		// Initiate an image change by setting condition and event recording
		o.Initiator.StartImageChange,
		// Do a clean shutdown of the cluster
		o.stopCluster,
		// Set the new image in the statefulset objects.
		o.updateImageInStatefulSets,
		// Delete pods that have the old image.
		o.deletePods,
		// Check for the pods to be created by the sts controller with the new image
		o.checkForNewPods,
		// Start up vertica in each pod.
		o.restartCluster,
		// Cleanup up the condition and event recording for a completed image change
		o.Initiator.FinishImageChange,
	}
	for _, fn := range funcs {
		if res, err := fn(ctx); res.Requeue || err != nil {
			return res, err
		}
	}

	return ctrl.Result{}, nil
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

	if err := o.setImageChangeStatus(ctx, "Starting cluster shutdown"); err != nil {
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

// updateImageInStatefulSets will update the statefulsets to have the new image.
// This depends on the statefulsets having the UpdateStrategy of OnDelete.
// Since there will be processing after to delete the pods so that they come up
// with the new image.
func (o *OfflineImageChangeReconciler) updateImageInStatefulSets(ctx context.Context) (ctrl.Result, error) {
	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	stss, err := o.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range stss.Items {
		sts := &stss.Items[i]
		// Skip the statefulset if it already has the proper image.
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != o.Vdb.Spec.Image {
			o.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
			err = o.setImageChangeStatus(ctx, "Rescheduling pods with new image name")
			if err != nil {
				return ctrl.Result{}, err
			}
			sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image = o.Vdb.Spec.Image
			// We change the update strategy to OnDelete.  We don't want the k8s
			// sts controller to interphere and do a rolling update after the
			// update has completed.  We don't explicitly change this back.  The
			// ObjReconciler will handle it for us.
			sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
			err = o.VRec.Client.Update(ctx, sts)
			if err != nil {
				return ctrl.Result{}, err
			}
			o.PFacts.Invalidate()
		}
	}
	return ctrl.Result{}, nil
}

// deletePods will delete pods that are running the old image.  The assumption
// is that the sts has already had its image updated and the UpdateStrategy for
// the sts is OnDelete.  Deleting the pods ensures they get rescheduled with the
// new image.
func (o *OfflineImageChangeReconciler) deletePods(ctx context.Context) (ctrl.Result, error) {
	// We use FindExisting for the finder because we only want to work with pods
	// that already exist.  This is necessary in case the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	pods, err := o.Finder.FindPods(ctx, FindExisting)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range pods.Items {
		pod := &pods.Items[i]
		// Skip the pod if it already has the proper image.
		if pod.Spec.Containers[names.ServerContainerIndex].Image != o.Vdb.Spec.Image {
			o.Log.Info("Deleting pod that had old image", "name", pod.ObjectMeta.Name)
			err = o.VRec.Client.Delete(ctx, pod)
			if err != nil {
				return ctrl.Result{}, err
			}
			o.PFacts.Invalidate()
		}
	}
	return ctrl.Result{}, nil
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

// restartCluster will start up vertica.  This is called after the statefulset's have
// been recreated.  Once the cluster is back up, then the image change is considered complete.
func (o *OfflineImageChangeReconciler) restartCluster(ctx context.Context) (ctrl.Result, error) {
	o.Log.Info("Starting restart phase of image change for this reconcile iteration")
	if err := o.setImageChangeStatus(ctx, "Restarting cluster"); err != nil {
		return ctrl.Result{}, err
	}
	// The restart reconciler is called after this reconciler.  But we call the
	// restart reconciler here so that we restart while the status condition is set.
	r := MakeRestartReconciler(o.VRec, o.Log, o.Vdb, o.PRunner, o.PFacts)
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

// setImageChangeStatus is a helper to set the imageChangeStatus message.
func (o *OfflineImageChangeReconciler) setImageChangeStatus(ctx context.Context, msg string) error {
	// SPILLY - move this to initiator?
	return status.UpdateImageChangeStatus(ctx, o.VRec.Client, o.Vdb, msg)
}

// IsAllowedForImageChangePolicy will determine if offline image change is
// allowed based on the policy in the Vdb
func (o *OfflineImageChangeReconciler) IsAllowedForImageChangePolicy(vdb *vapi.VerticaDB) bool {
	return offlineImageChangeAllowed(vdb)
}
