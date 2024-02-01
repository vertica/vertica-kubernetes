/*
 (c) Copyright [2021-2023] Open Text.
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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CrashLoopReconciler will check if a pod is in a crash loop due to a bad
// VClusterOps deployment. If found, then this reconciler will surface
// meaningful debug information.
type CrashLoopReconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	VDB  *vapi.VerticaDB
}

// MakeCrashLoopReconciler will build a reconcile actor for CrashLoopReconciler
func MakeCrashLoopReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &CrashLoopReconciler{
		VRec: vdbrecon,
		Log:  log.WithName("CrashLoopReconciler"),
		VDB:  vdb,
	}
}

// Reconcile will check for a crash loop in vclusterOps deployments.
func (v *CrashLoopReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// This reconciler is intended to find cases where the image does not have
	// the nma, causing the pod to get into a CrashLoop backoff. So, if not
	// deployed with vclusterOps we can skip this entirely.
	if !vmeta.UseVClusterOps(v.VDB.Annotations) {
		return ctrl.Result{}, nil
	}

	// We have to be careful about logging events about the wrong deployment type
	// for other kinds of crash loops. If the deployment is wrong it should
	// happen before the version annotation has been set, or for the case
	// of upgrade its still set to the old version. Exit out if the version in
	// the annotation, if present, supports vclusterOps.
	vinf, ok := v.VDB.MakeVersionInfo()
	if ok && vinf.IsEqualOrNewer(vapi.VcluseropsAsDefaultDeploymentMethodMinVersion) {
		return ctrl.Result{}, nil
	}

	v.reconcileStatefulSets(ctx)
	return ctrl.Result{}, nil
}

func (v *CrashLoopReconciler) reconcileStatefulSets(ctx context.Context) {
	finder := iter.MakeSubclusterFinder(v.VRec.Client, v.VDB)
	stss, err := finder.FindStatefulSets(ctx, iter.FindExisting|iter.FindSorted)
	if err != nil {
		// This reconciler is a best effort. It only tries to surface meaningful
		// error messages based on the events it see. For this reason, no errors
		// are emitted. We will log them then carry on to the next reconciler.
		v.Log.Info("Failure detecting in CrashLoopReconciler. Will continue on", "err", err)
		return
	}
	for i := range stss.Items {
		sts := &stss.Items[i]
		for j := int32(0); j < *sts.Spec.Replicas; j++ {
			pn := names.GenPodNameFromSts(v.VDB, sts, j)
			pod := &corev1.Pod{}
			err = v.VRec.Client.Get(ctx, pn, pod)
			if err != nil {
				// Any error found during fetch are ignored. We will just go onto the next pod.
				continue
			}
			nmaStatus := v.findNMAContainerStatus(pod)
			if nmaStatus == nil {
				continue
			}
			if nmaStatus.RestartCount > 0 &&
				!nmaStatus.Ready &&
				nmaStatus.LastTerminationState.Terminated != nil &&
				nmaStatus.LastTerminationState.Terminated.Reason == "StartError" {
				v.VRec.Eventf(v.VDB, corev1.EventTypeWarning, events.WrongImage,
					"Image cannot be used for vclusterOps deployments. Change the deployment by changing the %s annotation",
					vmeta.VClusterOpsAnnotation)
				// Don't bother checking anymore pods as this setting is global for the CR.
				return
			}
		}
	}
}

func (v *CrashLoopReconciler) findNMAContainerStatus(pod *corev1.Pod) *corev1.ContainerStatus {
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == names.NMAContainer {
			return &pod.Status.ContainerStatuses[i]
		}
	}
	return nil
}
