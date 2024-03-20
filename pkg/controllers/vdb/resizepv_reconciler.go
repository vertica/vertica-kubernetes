/*
 (c) Copyright [2021-2024] Open Text.
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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ResizePVReconcile will handle resizing of the PV when the local data size changes
type ResizePVReconcile struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB
	Log     logr.Logger
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeResizePVReconciler will build and return the ResizePVReconcile object.
func MakeResizePVReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &ResizePVReconcile{
		VRec:    vdbrecon,
		Vdb:     vdb,
		Log:     log.WithName("ResizePVReconciler"),
		PRunner: prunner,
		PFacts:  pfacts,
	}
}

// Reconcile will ensure Vertica is installed and running in the pods.
func (r *ResizePVReconcile) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	returnRes := ctrl.Result{}
	for _, pf := range r.PFacts.Detail {
		if res, err := r.reconcilePod(ctx, pf); verrors.IsReconcileAborted(res, err) {
			// Errors always abort right away.  But if we get a requeue, we
			// will remember this and go onto the next pod
			if err != nil {
				return res, err
			}
			returnRes = res
		}
	}

	return returnRes, nil
}

// reconcilePod will handle a single pod to see if its PV needs to be resized
func (r *ResizePVReconcile) reconcilePod(ctx context.Context, pf *PodFact) (ctrl.Result, error) {
	pvcName := types.NamespacedName{
		Namespace: pf.name.Namespace,
		Name:      fmt.Sprintf("%s-%s", vapi.LocalDataPVC, pf.name.Name),
	}
	pvc := &corev1.PersistentVolumeClaim{}
	if err := r.VRec.Client.Get(ctx, pvcName, pvc); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("PVC was not found. Requeuing.", "pvc", pvcName)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	return r.reconcilePvc(ctx, pf, pvc)
}

// reconcilePvc will handle a single PVC and see if it needs to be resized
func (r *ResizePVReconcile) reconcilePvc(ctx context.Context, pf *PodFact, pvc *corev1.PersistentVolumeClaim) (ctrl.Result, error) {
	// Resize is necessary if the PVC storage is smaller than the size in the vdb
	if pvc.Spec.Resources.Requests.Storage().Cmp(r.Vdb.Spec.Local.RequestSize) < 0 {
		return r.updatePVC(ctx, pvc)
	}

	// We are done with the PVC if the spec <= capacity size in the PVC.  It
	// isn't a strict equality check because the actual size of the PVC may be
	// larger than what was requested.  GCP rounds up to the nearest GB for
	// instance.
	if pvc.Spec.Resources.Requests.Storage().Cmp(*pvc.Status.Capacity.Storage()) <= 0 {
		return r.updateDepotSize(ctx, pvc, pf)
	}

	// Requeue to wait for the PVC to be expanded.
	r.Log.Info("Wait for PVC to be expanded", "pvc", pvc.Name, "capacity", pvc.Status.Capacity.Storage())
	return ctrl.Result{Requeue: true}, nil
}

// updatePVC will update the PVCs size with the size in the vdb.
func (r *ResizePVReconcile) updatePVC(ctx context.Context, pvc *corev1.PersistentVolumeClaim) (ctrl.Result, error) {
	nm := types.NamespacedName{
		Name:      pvc.Name,
		Namespace: pvc.Namespace,
	}
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		fetchedPVC := &corev1.PersistentVolumeClaim{}
		if err := r.VRec.Client.Get(ctx, nm, fetchedPVC); err != nil {
			return err
		}

		fetchedPVC.Spec.Resources.Requests[corev1.ResourceStorage] = r.Vdb.Spec.Local.RequestSize
		return r.VRec.Client.Update(ctx, fetchedPVC)
	})

	// Some storage classes forbid volume expansion.  We will ignore those
	// errors so that we can finish up the reconciler.
	if err != nil {
		k8sError, ok := err.(errors.APIStatus)
		if ok && k8sError.Status().Reason == metav1.StatusReasonForbidden {
			r.Log.Info("Skipping expansion of PVC because volume expansion is forbidden", "pvc", pvc.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}
	r.Log.Info("PVC resized, so requeue iteration to wait for PV to get resized.", "pvc", pvc.Name)
	return ctrl.Result{Requeue: true}, nil
}

// updateDepotSize will call alter_location_size in vertica if necessary
func (r *ResizePVReconcile) updateDepotSize(ctx context.Context, pvc *corev1.PersistentVolumeClaim,
	pf *PodFact) (ctrl.Result, error) {
	if r.Vdb.IsDepotVolumeEmptyDir() {
		r.Log.Info("Skipping depot resize because its volume is an emptyDir", "pod", pf.name.Name)
		return ctrl.Result{}, nil
	}
	if !pf.upNode {
		r.Log.Info("Depot size needs to be checked in vertica. Requeue to wait for vertica to come up")
		return ctrl.Result{Requeue: true}, nil
	}
	if pf.depotDiskPercentSize == "" {
		r.Log.Info("Skipping depot resize because its size is fixed and not a percentage of the disk space.", "pod", pf.name.Name)
		return ctrl.Result{}, nil
	}
	if !strings.HasSuffix(pf.depotDiskPercentSize, "%") {
		return ctrl.Result{}, fmt.Errorf("depot disk percent must end with %%: %s", pf.depotDiskPercentSize)
	}
	dpAsInt, err := strconv.Atoi(pf.depotDiskPercentSize[:len(pf.depotDiskPercentSize)-1])
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("cannot convert depot disk percent (%s) to an int: %w", pf.depotDiskPercentSize, err)
	}
	curLocalDataSize, err := r.getLocalDataSize(pvc, pf)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Calculate a lowerbound of the expected depot size.  We calculate it with
	// the capacity of the local disk then reduce it by a constant amount.  This
	// fudge is here in case Vertica and our operator calculate the expected
	// depot size differently (i.e. rounding, etc.)
	depotSizeLB := (curLocalDataSize * int64(dpAsInt) / 100) - (5 * 1024 * 1024)
	if int64(pf.maxDepotSize) >= depotSizeLB {
		r.Log.Info("Depot resize isn't needed in Vertica",
			"cur depot size", pf.maxDepotSize, "expected depot size", depotSizeLB)
		return ctrl.Result{}, nil
	}
	r.Log.Info("alter_location_size needed", "curLocalDataSize", curLocalDataSize,
		"maxDepotSize", pf.maxDepotSize, "depotSizeLB", depotSizeLB)
	sql := []string{
		"-tAc",
		fmt.Sprintf("select alter_location_size('depot', '%s', '%s')",
			pf.vnodeName, pf.depotDiskPercentSize),
	}
	_, _, err = r.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, sql...)
	if err == nil {
		r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.DepotResized,
			"Depot was resized in pod '%s' to be %s of expanded PVC", pf.name.Name, pf.depotDiskPercentSize)
	}
	return ctrl.Result{}, err
}

// getLocalDataSize returns the size of the mount that contains the depot
func (r *ResizePVReconcile) getLocalDataSize(pvc *corev1.PersistentVolumeClaim, pf *PodFact) (int64, error) {
	// If the output is empty, we will use the size from the PVC.  These is here
	// for test purposes.  The PVC capacity was close to 100mb larger than then
	// disk size that Vertica calculates, which is why it isn't preferred way of
	// calculating.
	if pf.localDataSize == 0 {
		curCapacity, ok := pvc.Status.Capacity.Storage().AsInt64()
		if !ok {
			return 0, fmt.Errorf("cannot get capacity as int64: %s", pvc.Status.Capacity.Storage().String())
		}
		return curCapacity, nil
	}
	return int64(pf.localDataSize), nil
}
