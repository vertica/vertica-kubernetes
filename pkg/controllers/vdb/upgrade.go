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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

type UpgradeManager struct {
	VRec              *VerticaDBReconciler
	Vdb               *vapi.VerticaDB
	Log               logr.Logger
	Finder            iter.SubclusterFinder
	ContinuingUpgrade bool // true if UpdateInProgress was already set upon entry
	StatusCondition   string
	// Function that will check if the image policy allows for a type of upgrade (offline or online)
	IsAllowedForUpgradePolicyFunc func(vdb *vapi.VerticaDB) bool
	PrimaryImages                 []string // Known images in the primaries.  Should be of length 1 or 2.
}

// MakeUpgradeManager will construct a UpgradeManager object
func MakeUpgradeManager(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	statusCondition string,
	isAllowedForUpgradePolicyFunc func(vdb *vapi.VerticaDB) bool) *UpgradeManager {
	return &UpgradeManager{
		VRec:                          vdbrecon,
		Vdb:                           vdb,
		Log:                           log,
		Finder:                        iter.MakeSubclusterFinder(vdbrecon.Client, vdb),
		StatusCondition:               statusCondition,
		IsAllowedForUpgradePolicyFunc: isAllowedForUpgradePolicyFunc,
	}
}

// IsUpgradeNeeded checks whether an upgrade is needed and/or in
// progress.  It will return true for the first parm if an upgrade should
// proceed.
func (i *UpgradeManager) IsUpgradeNeeded(ctx context.Context) (bool, error) {
	// no-op for ScheduleOnly init policy
	if i.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return false, nil
	}

	if ok := i.isUpgradeInProgress(); ok {
		return ok, nil
	}

	if ok := i.IsAllowedForUpgradePolicyFunc(i.Vdb); !ok {
		return ok, nil
	}

	return i.isVDBImageDifferent(ctx)
}

// isUpgradeInProgress returns true if state indicates that an upgrade
// is already occurring.
func (i *UpgradeManager) isUpgradeInProgress() bool {
	// We first check if the status condition indicates the upgrade is in progress
	isSet := i.Vdb.IsStatusConditionTrue(i.StatusCondition)
	if isSet {
		i.ContinuingUpgrade = true
	}
	return isSet
}

// isVDBImageDifferent will check if an upgrade is needed based on the
// image being different between the Vdb and any of the statefulset's.
func (i *UpgradeManager) isVDBImageDifferent(ctx context.Context) (bool, error) {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindInVdb)
	if err != nil {
		return false, err
	}
	for inx := range stss.Items {
		sts := stss.Items[inx]
		cntImage, err := vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)
		if err != nil {
			return false, err
		}
		if cntImage != i.Vdb.Spec.Image {
			return true, nil
		}
	}

	return false, nil
}

// startUpgrade handles condition status and event recording for start of an upgrade
func (i *UpgradeManager) startUpgrade(ctx context.Context) (ctrl.Result, error) {
	i.Log.Info("Starting upgrade for reconciliation iteration", "ContinuingUpgrade", i.ContinuingUpgrade,
		"New Image", i.Vdb.Spec.Image)
	if err := i.toggleUpgradeInProgress(ctx, metav1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message and bump a counter the first time we begin an upgrade.
	if !i.ContinuingUpgrade {
		i.VRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeStart,
			"Vertica server upgrade has started.")
		metrics.UpgradeCount.With(metrics.MakeVDBLabels(i.Vdb)).Inc()
	}
	return ctrl.Result{}, nil
}

// finishUpgrade handles condition status and event recording for the end of an upgrade
func (i *UpgradeManager) finishUpgrade(ctx context.Context) (ctrl.Result, error) {
	if err := i.setUpgradeStatus(ctx, ""); err != nil {
		return ctrl.Result{}, err
	}

	if err := i.clearReplicatedUpgradeAnnotations(ctx); err != nil {
		return ctrl.Result{}, err
	}

	if err := i.toggleUpgradeInProgress(ctx, metav1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	i.Log.Info("The upgrade has completed successfully")
	i.VRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeSucceeded,
		"Vertica server upgrade has completed successfully.  New image is '%s'", i.Vdb.Spec.Image)

	return ctrl.Result{}, nil
}

// toggleUpgradeInProgress is a helper for updating the
// UpgradeInProgress condition's.  We set the UpgradeInProgress plus the
// one defined in i.StatusCondition.
func (i *UpgradeManager) toggleUpgradeInProgress(ctx context.Context, newVal metav1.ConditionStatus) error {
	reason := "UpgradeStarted"
	if newVal == metav1.ConditionFalse {
		reason = "UpgradeFinished"
	}
	err := vdbstatus.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.MakeCondition(vapi.UpgradeInProgress, newVal, reason),
	)
	if err != nil {
		return err
	}
	return vdbstatus.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.MakeCondition(i.StatusCondition, newVal, reason),
	)
}

// setUpgradeStatus is a helper to set the upgradeStatus message.
func (i *UpgradeManager) setUpgradeStatus(ctx context.Context, msg string) error {
	return vdbstatus.SetUpgradeStatusMessage(ctx, i.VRec.Client, i.Vdb, msg)
}

// clearReplicatedUpgradeAnnotations will clear the annotation we set for replicated upgrade
func (i *UpgradeManager) clearReplicatedUpgradeAnnotations(ctx context.Context) error {
	_, err := i.updateVDBWithRetry(ctx, i.clearReplicatedUpgradeAnnotationCallback)
	return err
}

// clearReplicatedUpgradeAnnotationCallback is a callback function to perform
// the actual update to the VDB. It will remove all annotations used by
// replicated upgrade.
func (i *UpgradeManager) clearReplicatedUpgradeAnnotationCallback() (updated bool, err error) {
	for inx := range i.Vdb.Spec.Subclusters {
		sc := &i.Vdb.Spec.Subclusters[inx]
		for _, a := range []string{vmeta.ReplicaGroupAnnotation, vmeta.ChildSubclusterAnnotation, vmeta.ParentSubclusterAnnotation} {
			if _, annotationFound := sc.Annotations[a]; annotationFound {
				delete(sc.Annotations, a)
				updated = true
			}
		}
	}

	// Clear annotations set in the VerticaDB's metadata.annotations.
	for _, a := range []string{vmeta.ReplicatedUpgradeReplicatorAnnotation, vmeta.ReplicatedUpgradeSandboxAnnotation} {
		if _, annotationFound := i.Vdb.Annotations[a]; annotationFound {
			delete(i.Vdb.Annotations, a)
			updated = true
		}
	}
	return
}

// updateImageInStatefulSets will change the image in each of the statefulsets.
// This changes the images in all subclusters except any transient ones.
func (i *UpgradeManager) updateImageInStatefulSets(ctx context.Context) (int, error) {
	numStsChanged := 0 // Count to keep track of the nubmer of statefulsets updated

	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the upgrade was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the upgrade.
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting)
	if err != nil {
		return numStsChanged, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		isTransient, err := strconv.ParseBool(sts.Labels[vmeta.SubclusterTransientLabel])
		if err != nil {
			return numStsChanged, err
		}
		if isTransient {
			continue
		}

		if stsUpdated, err := i.updateImageInStatefulSet(ctx, sts); err != nil {
			return numStsChanged, err
		} else if stsUpdated {
			numStsChanged++
		}
	}
	return numStsChanged, nil
}

// updateImageInStatefulSet will update the image in the given statefulset.  It
// returns true if the image was changed.
func (i *UpgradeManager) updateImageInStatefulSet(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	stsUpdated := false
	// Skip the statefulset if it already has the proper image.
	svrCnt := vk8s.GetServerContainer(sts.Spec.Template.Spec.Containers)
	if svrCnt == nil {
		return false, fmt.Errorf("could not find the server container in the sts %s", sts.Name)
	}
	if svrCnt.Image != i.Vdb.Spec.Image {
		i.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
		svrCnt.Image = i.Vdb.Spec.Image
		if nmaCnt := vk8s.GetNMAContainer(sts.Spec.Template.Spec.Containers); nmaCnt != nil {
			nmaCnt.Image = i.Vdb.Spec.Image
		}
		// We change the update strategy to OnDelete.  We don't want the k8s
		// sts controller to interphere and do a rolling update after the
		// update has completed.  We don't explicitly change this back.  The
		// ObjReconciler will handle it for us.
		sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
		if err := i.VRec.Client.Update(ctx, sts); err != nil {
			return false, err
		}
		stsUpdated = true
	}
	return stsUpdated, nil
}

// deletePodsRunningOldImage will delete pods that have the old image.  It will return the
// number of pods that were deleted.  Callers can control whether to delete pods
// for a specific subcluster or all -- passing an empty string for scName will delete all.
func (i *UpgradeManager) deletePodsRunningOldImage(ctx context.Context, scName string) (int, error) {
	numPodsDeleted := 0 // Tracks the number of pods that were deleted

	// We use FindExisting for the finder because we only want to work with pods
	// that already exist.  This is necessary in case the upgrade was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the upgrade.
	pods, err := i.Finder.FindPods(ctx, iter.FindExisting, vapi.MainCluster)
	if err != nil {
		return numPodsDeleted, err
	}
	for inx := range pods.Items {
		pod := &pods.Items[inx]

		// If scName was passed in, we only delete for a specific subcluster
		if scName != "" {
			scNameFromLabel, ok := pod.Labels[vmeta.SubclusterNameLabel]
			if ok && scNameFromLabel != scName {
				continue
			}
		}

		// Skip the pod if it already has the proper image.
		cntImage, err := vk8s.GetServerImage(pod.Spec.Containers)
		if err != nil {
			return numPodsDeleted, err
		}
		if cntImage != i.Vdb.Spec.Image {
			i.Log.Info("Deleting pod that had old image", "name", pod.ObjectMeta.Name)
			err = i.VRec.Client.Delete(ctx, pod)
			if err != nil {
				return numPodsDeleted, err
			}
			numPodsDeleted++
		}
	}
	return numPodsDeleted, nil
}

// deleteStsRunningOldImage will delete statefulsets that have the old image.
func (i *UpgradeManager) deleteStsRunningOldImage(ctx context.Context) error {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting)
	if err != nil {
		return err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		cntImage, err := vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)
		if err != nil {
			return err
		}
		if cntImage != i.Vdb.Spec.Image {
			i.Log.Info("Deleting sts that had old image", "name", sts.ObjectMeta.Name)
			err = i.VRec.Client.Delete(ctx, sts)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// changeNMASidecarDeploymentIfNeeded will handle the case where we are
// upgrading across versions such that we need to deploy the NMA sidecar.
func (i *UpgradeManager) changeNMASidecarDeploymentIfNeeded(ctx context.Context, sts *appsv1.StatefulSet) (ctrl.Result, error) {
	// Early out if the sts already has an NMA sidecar
	if vk8s.HasNMAContainer(&sts.Spec.Template.Spec) {
		return ctrl.Result{}, nil
	}
	i.Log.Info("Checking if NMA sidecar deployment is changing")

	// Check the state of the first pod in the sts.
	pn := names.GenPodNameFromSts(i.Vdb, sts, 0)
	pod := &corev1.Pod{}
	err := i.VRec.Client.Get(ctx, pn, pod)
	if err != nil {
		return ctrl.Result{}, err
	}

	serverContainer := vk8s.FindServerContainerStatus(pod)
	if serverContainer == nil {
		return ctrl.Result{}, fmt.Errorf("could not find server container in pod spec of %s", pn.Name)
	}
	if serverContainer.Ready ||
		(serverContainer.Started != nil && *serverContainer.Started) ||
		!vk8s.HasCreateContainerError(serverContainer) {
		return ctrl.Result{}, nil
	}
	// Sadly if we determine that we need to change and deploy the NMA sidecar,
	// we need to apply this across all subcluster. This effectively makes the
	// upgrade offline. The way we trigger the new NMA sidecar is by updating
	// the version in the VerticaDB. Since this is a CR wide value, it applies to
	// all subclusters.
	i.Log.Info("Detected that we need to switch to NMA sidecar. Deleting all sts")
	err = i.deleteStsRunningOldImage(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	// Change the vdb version to the first one that supports the NMA sidecar.
	// This is likely not the correct version. But it at least will force
	// creation of a sts with a sidecar. The actual true version will replace
	// this dummy version we setup once the pods are running.
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err = i.VRec.Client.Get(ctx, i.Vdb.ExtractNamespacedName(), i.Vdb)
		if err != nil {
			return err
		}
		if i.Vdb.Annotations == nil {
			i.Vdb.Annotations = map[string]string{}
		}
		i.Vdb.Annotations[vmeta.VersionAnnotation] = vapi.NMAInSideCarDeploymentMinVersion
		i.Log.Info("Force a dummy version in the vdb to ensure NMA sidecar is created", "version", i.Vdb.Annotations[vmeta.VersionAnnotation])
		return i.VRec.Client.Update(ctx, i.Vdb)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// updateVDBWithRetry will update the VDB by way of a callback. This is done in a retry
// loop in case there is a write conflict.
func (i *UpgradeManager) updateVDBWithRetry(ctx context.Context, callbackFn func() (bool, error)) (updated bool, err error) {
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err := i.VRec.Get(ctx, i.Vdb.ExtractNamespacedName(), i.Vdb)
		if err != nil {
			return err
		}

		needToUpdate, err := callbackFn()
		if err != nil {
			return err
		}

		if !needToUpdate {
			return nil
		}
		err = i.VRec.Update(ctx, i.Vdb)
		if err == nil {
			updated = true
		}
		return err
	})
	return
}

// postNextStatusMsg will set the next status message.  This will only
// transition to a message, defined by msgIndex, if the current status equals
// the previous one.
func (i *UpgradeManager) postNextStatusMsg(ctx context.Context, statusMsgs []string, msgIndex int) error {
	if msgIndex >= len(statusMsgs) {
		return fmt.Errorf("msgIndex out of bounds: %d must be between %d and %d", msgIndex, 0, len(statusMsgs)-1)
	}

	if msgIndex == 0 {
		if i.Vdb.Status.UpgradeStatus == "" {
			return i.setUpgradeStatus(ctx, statusMsgs[msgIndex])
		}
		return nil
	}

	// Compare with all status messages prior to msgIndex.  The current status
	// in the vdb might not be the proceeding one if the vdb is stale.
	for j := 0; j <= msgIndex-1; j++ {
		if statusMsgs[j] == i.Vdb.Status.UpgradeStatus {
			err := i.setUpgradeStatus(ctx, statusMsgs[msgIndex])
			i.Log.Info("Status message after update", "msgIndex", msgIndex, "statusMsgs[msgIndex]", statusMsgs[msgIndex],
				"UpgradeStatus", i.Vdb.Status.UpgradeStatus, "err", err)
			return err
		}
	}
	return nil
}

// offlineUpgradeAllowed returns true if upgrade must be done offline
func offlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.OfflineUpgrade
}

// onlineUpgradeAllowed returns true if upgrade must be done online
func onlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.OnlineUpgrade
}

// replicatedUpgradeAllowed returns true if upgrade must be done with the
// replicated upgrade strategy.
func replicatedUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.ReplicatedUpgrade
}

// cachePrimaryImages will update o.PrimaryImages with the names of all of the primary images
func (i *UpgradeManager) cachePrimaryImages(ctx context.Context) error {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting)
	if err != nil {
		return err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]
		if sts.Labels[vmeta.SubclusterTypeLabel] == vapi.PrimarySubcluster {
			img, err := vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)
			if err != nil {
				return err
			}
			imageFound := false
			for j := range i.PrimaryImages {
				imageFound = i.PrimaryImages[j] == img
				if imageFound {
					break
				}
			}
			if !imageFound {
				i.PrimaryImages = append(i.PrimaryImages, img)
			}
		}
	}
	return nil
}

// fetchOldImage will return the old image that existed prior to the image
// change process.  If we cannot determine the old image, then the bool return
// value returns false.
func (i *UpgradeManager) fetchOldImage() (string, bool) {
	for inx := range i.PrimaryImages {
		if i.PrimaryImages[inx] != i.Vdb.Spec.Image {
			return i.PrimaryImages[inx], true
		}
	}
	return "", false
}

func (i *UpgradeManager) traceActorReconcile(actor controllers.ReconcileActor) {
	i.Log.Info("starting actor for upgrade", "name", fmt.Sprintf("%T", actor))
}

// isSubclusterIdle will run a query to see the number of connections
// that are active for a given subcluster.  It returns a requeue error if there
// are active connections still.
func (i *UpgradeManager) isSubclusterIdle(ctx context.Context, pfacts *PodFacts, scName string) (ctrl.Result, error) {
	pf, ok := pfacts.findPodToRunVsql(true, scName)
	if !ok {
		i.Log.Info("No pod found to run vsql.  Skipping active connection check")
		return ctrl.Result{}, nil
	}

	sql := fmt.Sprintf(
		"select count(session_id) sessions"+
			" from v_monitor.sessions join v_catalog.subclusters using (node_name)"+
			" where session_id not in (select session_id from current_session)"+
			"       and subcluster_name = '%s';", scName)

	cmd := []string{"-tAc", sql}
	stdout, _, err := pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Parse the output.  We requeue if there is an active connection.  This
	// will rely on the UpgradeRequeueTime that is set to default
	res := ctrl.Result{Requeue: anyActiveConnections(stdout)}
	if res.Requeue {
		i.VRec.Eventf(i.Vdb, corev1.EventTypeWarning, events.DrainSubclusterRetry,
			"Subcluster '%s' has active connections preventing the drain from succeeding", scName)
	}
	return res, nil
}

// routeClientTraffic will update service objects for the source subcluster to
// route to the target subcluster
func (i *UpgradeManager) routeClientTraffic(ctx context.Context, pfacts *PodFacts, sc *vapi.Subcluster, selectors map[string]string) error {
	actor := MakeObjReconciler(i.VRec, i.Log, i.Vdb, pfacts, ObjReconcileModeAll)
	objRec := actor.(*ObjReconciler)

	// We update the external service object to route traffic to the target
	// subcluster. If sourceSc is the same as targetSc, this will update the
	// service object so it routes to the source. Kind of like undoing a
	// temporary routing decision.
	//
	// We are only concerned with changing the labels.  So we will fetch the
	// current service object, then update the labels so that traffic diverted
	// to the correct statefulset.  Other things, such as service type, stay the same.
	svcName := names.GenExtSvcName(i.Vdb, sc)
	svc := &corev1.Service{}
	if err := i.VRec.Client.Get(ctx, svcName, svc); err != nil {
		if errors.IsNotFound(err) {
			i.Log.Info("Skipping client traffic routing because service object for subcluster not found",
				"scName", sc.Name, "svc", svcName)
			return nil
		}
		return err
	}

	svc.Spec.Selector = selectors
	i.Log.Info("Updating svc", "selector", svc.Spec.Selector)
	return objRec.reconcileExtSvc(ctx, svc, sc)
}
