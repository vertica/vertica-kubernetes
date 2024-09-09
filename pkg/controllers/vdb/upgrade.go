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

	"errors"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	statusConditionEmpty = ""
)

type UpgradeManager struct {
	Rec               config.ReconcilerInterface
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
func MakeUpgradeManager(recon config.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB,
	statusCondition string,
	isAllowedForUpgradePolicyFunc func(vdb *vapi.VerticaDB) bool) *UpgradeManager {
	return &UpgradeManager{
		Rec:                           recon,
		Vdb:                           vdb,
		Log:                           log,
		Finder:                        iter.MakeSubclusterFinder(recon.GetClient(), vdb),
		StatusCondition:               statusCondition,
		IsAllowedForUpgradePolicyFunc: isAllowedForUpgradePolicyFunc,
	}
}

// MakeUpgradeManagerForSandboxOffline will construct a UpgradeManager object for
// sandbox offline upgrade only
func MakeUpgradeManagerForSandboxOffline(recon config.ReconcilerInterface, log logr.Logger, vdb *vapi.VerticaDB,
	statusCondition string) *UpgradeManager {
	// For sandbox upgrade, offline upgrade path must always be selected regardless
	// of the upgrade policy set in vdb. Moreover, we don't use status conditions
	// during sandbox upgrade
	return MakeUpgradeManager(recon, log, vdb, statusConditionEmpty, func(vdb *vapi.VerticaDB) bool { return true })
}

// IsUpgradeNeeded checks whether an upgrade is needed and/or in
// progress.  It will return true for the first parm if an upgrade should
// proceed.
func (i *UpgradeManager) IsUpgradeNeeded(ctx context.Context, sandbox string) (bool, error) {
	// no-op for ScheduleOnly init policy
	if i.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return false, nil
	}

	if ok := i.isUpgradeInProgress(sandbox); ok {
		return ok, nil
	}

	if ok := i.IsAllowedForUpgradePolicyFunc(i.Vdb); !ok {
		return ok, nil
	}

	return i.isVDBImageDifferent(ctx, sandbox)
}

// isUpgradeInProgress returns true if state indicates that an upgrade
// is already occurring.
func (i *UpgradeManager) isUpgradeInProgress(sbName string) bool {
	// We first check if the status condition indicates the upgrade is in progress
	isSet := i.isUpgradeStatusTrue(sbName)
	if isSet {
		i.ContinuingUpgrade = true
	}
	return isSet
}

func (i *UpgradeManager) isUpgradeStatusTrue(sbName string) bool {
	if sbName == vapi.MainCluster {
		return i.Vdb.IsStatusConditionTrue(i.StatusCondition)
	}
	return i.Vdb.IsSandBoxUpgradeInProgress(sbName)
}

// isVDBImageDifferent will check if an upgrade is needed based on the
// image being different between the Vdb and any of the statefulset's or
// between a sandbox and any of its statefulsets if the sandbox name is non-empty.
func (i *UpgradeManager) isVDBImageDifferent(ctx context.Context, sandbox string) (bool, error) {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindInVdb, sandbox)
	if err != nil {
		return false, err
	}
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return false, err
	}
	for inx := range stss.Items {
		sts := stss.Items[inx]
		cntImage, err := vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)
		if err != nil {
			return false, err
		}
		if cntImage != targetImage {
			return true, nil
		}
	}

	return false, nil
}

// logUpgradeStarted logs an event msg when upgrade is sstarting
func (i *UpgradeManager) logUpgradeStarted(sandbox string) error {
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return err
	}
	i.Log.Info("Starting upgrade for reconciliation iteration", "ContinuingUpgrade", i.ContinuingUpgrade,
		"New Image", targetImage, "Sandbox", sandbox)
	return nil
}

// startUpgrade handles condition status and event recording for start of an upgrade
func (i *UpgradeManager) startUpgrade(ctx context.Context, sbName string) (ctrl.Result, error) {
	if err := i.toggleUpgradeInProgress(ctx, metav1.ConditionTrue, sbName); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message and bump a counter the first time we begin an upgrade.
	if !i.ContinuingUpgrade {
		i.Rec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeStart,
			"Vertica server upgrade has started.")
		metrics.UpgradeCount.With(metrics.MakeVDBLabels(i.Vdb)).Inc()
	}
	return ctrl.Result{}, nil
}

// finishUpgrade handles condition status and event recording for the end of an upgrade
func (i *UpgradeManager) finishUpgrade(ctx context.Context, sbName string) (ctrl.Result, error) {
	if err := i.setUpgradeStatus(ctx, "", sbName); err != nil {
		return ctrl.Result{}, err
	}

	// We need to clear some annotations after online upgrade.
	if i.StatusCondition == vapi.OnlineUpgradeInProgress {
		if err := i.clearOnlineUpgradeAnnotations(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := i.toggleUpgradeInProgress(ctx, metav1.ConditionFalse, sbName); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// logUpgradeSucceeded logs an event msg when upgrade is successful
func (i *UpgradeManager) logUpgradeSucceeded(sandbox string) error {
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return err
	}
	i.Log.Info("The upgrade has completed successfully", "Sandbox", sandbox)
	i.Rec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeSucceeded,
		"Vertica server upgrade has completed successfully.  New image is '%s'", targetImage)
	return nil
}

// toggleUpgradeInProgress is a helper for updating the
// UpgradeInProgress condition's.  We set the UpgradeInProgress plus the
// one defined in i.StatusCondition.
func (i *UpgradeManager) toggleUpgradeInProgress(ctx context.Context, newVal metav1.ConditionStatus, sbName string) error {
	reason := "UpgradeStarted"
	if newVal == metav1.ConditionFalse {
		reason = "UpgradeFinished"
	}
	return i.updateUpgradeStatus(ctx, newVal, reason, sbName)
}

// updateUpgradeStatus sets the upgrade status
func (i *UpgradeManager) updateUpgradeStatus(ctx context.Context, newVal metav1.ConditionStatus,
	reason, sbName string) error {
	if sbName == vapi.MainCluster {
		err := vdbstatus.UpdateCondition(ctx, i.Rec.GetClient(), i.Vdb,
			vapi.MakeCondition(vapi.UpgradeInProgress, newVal, reason),
		)
		if err != nil {
			return err
		}
		return vdbstatus.UpdateCondition(ctx, i.Rec.GetClient(), i.Vdb,
			vapi.MakeCondition(i.StatusCondition, newVal, reason),
		)
	}
	sb, err := i.Vdb.GetSandboxStatusCheck(sbName)
	if err != nil {
		return err
	}
	state := sb.UpgradeState.DeepCopy()
	state.UpgradeInProgress = newVal == metav1.ConditionTrue
	return vdbstatus.SetSandboxUpgradeState(ctx, i.Rec.GetClient(), i.Vdb, sbName, state)
}

// setUpgradeStatus is a helper to set the upgradeStatus message.
func (i *UpgradeManager) setUpgradeStatus(ctx context.Context, msg, sbName string) error {
	if sbName == vapi.MainCluster {
		return vdbstatus.SetUpgradeStatusMessage(ctx, i.Rec.GetClient(), i.Vdb, msg)
	}
	sb, err := i.Vdb.GetSandboxStatusCheck(sbName)
	if err != nil {
		return err
	}
	state := sb.UpgradeState.DeepCopy()
	state.UpgradeStatus = msg
	return vdbstatus.SetSandboxUpgradeState(ctx, i.Rec.GetClient(), i.Vdb, sbName, state)
}

// clearOnlineUpgradeAnnotations will clear the annotation we set for online upgrade
func (i *UpgradeManager) clearOnlineUpgradeAnnotations(ctx context.Context) error {
	_, err := vk8s.UpdateVDBWithRetry(ctx, i.Rec, i.Vdb, i.clearOnlineUpgradeAnnotationCallback)
	return err
}

// clearOnlineUpgradeAnnotationCallback is a callback function to perform
// the actual update to the VDB. It will remove all annotations used by
// online upgrade.
func (i *UpgradeManager) clearOnlineUpgradeAnnotationCallback() (updated bool, err error) {
	for inx := range i.Vdb.Spec.Subclusters {
		sc := &i.Vdb.Spec.Subclusters[inx]
		for _, a := range []string{vmeta.ReplicaGroupAnnotation,
			vmeta.ParentSubclusterAnnotation, vmeta.ParentSubclusterTypeAnnotation} {
			if _, annotationFound := sc.Annotations[a]; annotationFound {
				delete(sc.Annotations, a)
				updated = true
			}
		}
	}

	// Clear annotations set in the VerticaDB's metadata.annotations.
	for _, a := range []string{vmeta.OnlineUpgradeReplicatorAnnotation, vmeta.OnlineUpgradeSandboxAnnotation,
		vmeta.OnlineUpgradeStepInxAnnotation, vmeta.OnlineUpgradePreferredSandboxAnnotation,
		vmeta.OnlineUpgradePromotionAttemptAnnotation, vmeta.OnlineUpgradeArchiveAnnotation} {
		if _, annotationFound := i.Vdb.Annotations[a]; annotationFound {
			delete(i.Vdb.Annotations, a)
			updated = true
		}
	}
	return
}

// updateImageInStatefulSets will change the image in each of the statefulsets.
// This changes the images in all subclusters except any transient ones.
func (i *UpgradeManager) updateImageInStatefulSets(ctx context.Context, sandbox string) (int, error) {
	numStsChanged := 0 // Count to keep track of the nubmer of statefulsets updated

	transientName, hasTransient := i.Vdb.GetTransientSubclusterName()

	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the upgrade was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the upgrade.
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting, sandbox)
	if err != nil {
		return numStsChanged, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		if hasTransient && transientName == sts.Labels[vmeta.SubclusterNameLabel] {
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
	targetImage, err := i.getTargetImage(sts.Labels[vmeta.SandboxNameLabel])
	if err != nil {
		return false, err
	}
	if svrCnt.Image != targetImage {
		i.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
		svrCnt.Image = targetImage
		if nmaCnt := vk8s.GetNMAContainer(sts.Spec.Template.Spec.Containers); nmaCnt != nil {
			nmaCnt.Image = targetImage
		}
		// We change the update strategy to OnDelete.  We don't want the k8s
		// sts controller to interphere and do a rolling update after the
		// update has completed.  We don't explicitly change this back.  The
		// ObjReconciler will handle it for us.
		sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
		if err := i.Rec.GetClient().Update(ctx, sts); err != nil {
			return false, err
		}
		stsUpdated = true
	}
	return stsUpdated, nil
}

// deletePodsRunningOldImage will delete pods that have the old image.  It will return the
// number of pods that were deleted.  Callers can control whether to delete pods
// for a specific subcluster or all -- passing an empty string for scName will delete all.
func (i *UpgradeManager) deletePodsRunningOldImage(ctx context.Context, scName, sandbox string) (int, error) {
	i.Log.Info("deleting pods with old image", "sandbox", sandbox, "scName", scName)
	numPodsDeleted := 0 // Tracks the number of pods that were deleted

	// We use FindExisting for the finder because we only want to work with pods
	// that already exist.  This is necessary in case the upgrade was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the upgrade.
	pods, err := i.Finder.FindPods(ctx, iter.FindExisting, sandbox)
	if err != nil {
		return numPodsDeleted, err
	}
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return numPodsDeleted, err
	}
	for inx := range pods.Items {
		pod := &pods.Items[inx]

		// If scName was passed in, we only delete for a specific subcluster
		if scName != "" {
			stsName, found := pod.Labels[vmeta.SubclusterSelectorLabel]
			if !found {
				return 0, fmt.Errorf("could not derive the statefulset name from the pod %q", pod.Name)
			}
			scNameFromLabel, err := i.getSubclusterNameFromSts(ctx, stsName)
			if err != nil {
				return 0, err
			}

			if scNameFromLabel != scName {
				continue
			}
		}

		// Skip the pod if it already has the proper image.
		cntImage, err := vk8s.GetServerImage(pod.Spec.Containers)
		if err != nil {
			return numPodsDeleted, err
		}
		if cntImage != targetImage {
			i.Log.Info("Deleting pod that had old image", "name", pod.ObjectMeta.Name)
			err = i.Rec.GetClient().Delete(ctx, pod)
			if err != nil {
				return numPodsDeleted, err
			}
			numPodsDeleted++
		}
	}
	return numPodsDeleted, nil
}

// deleteStsRunningOldImage will delete statefulsets that have the old image.
func (i *UpgradeManager) deleteStsRunningOldImage(ctx context.Context, sandbox string) error {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting, sandbox)
	if err != nil {
		return err
	}
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		cntImage, err := vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)
		if err != nil {
			return err
		}
		if cntImage != targetImage {
			i.Log.Info("Deleting sts that had old image", "name", sts.ObjectMeta.Name)
			err = i.Rec.GetClient().Delete(ctx, sts)
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
	err := i.Rec.GetClient().Get(ctx, pn, pod)
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
	err = i.deleteStsRunningOldImage(ctx, sts.Labels[vmeta.SandboxNameLabel])
	if err != nil {
		return ctrl.Result{}, err
	}
	// Change the vdb version to the first one that supports the NMA sidecar.
	// This is likely not the correct version. But it at least will force
	// creation of a sts with a sidecar. The actual true version will replace
	// this dummy version we setup once the pods are running.
	err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		err = i.Rec.GetClient().Get(ctx, i.Vdb.ExtractNamespacedName(), i.Vdb)
		if err != nil {
			return err
		}
		if i.Vdb.Annotations == nil {
			i.Vdb.Annotations = map[string]string{}
		}
		i.Vdb.Annotations[vmeta.VersionAnnotation] = vapi.NMAInSideCarDeploymentMinVersion
		i.Log.Info("Force a dummy version in the vdb to ensure NMA sidecar is created", "version", i.Vdb.Annotations[vmeta.VersionAnnotation])
		return i.Rec.GetClient().Update(ctx, i.Vdb)
	})
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{Requeue: true}, nil
}

// postNextStatusMsg will set the next status message.  This will only
// transition to a message, defined by msgIndex, if the current status equals
// the previous one.
func (i *UpgradeManager) postNextStatusMsg(ctx context.Context, statusMsgs []string, msgIndex int, sbName string) error {
	if msgIndex >= len(statusMsgs) {
		return fmt.Errorf("msgIndex out of bounds: %d must be between %d and %d", msgIndex, 0, len(statusMsgs)-1)
	}

	upgradeStatus, err := i.Vdb.GetUpgradeStatus(sbName)
	if err != nil {
		return err
	}
	if msgIndex == 0 {
		if upgradeStatus == "" {
			return i.setUpgradeStatus(ctx, statusMsgs[msgIndex], sbName)
		}
		return nil
	}

	// Compare with all status messages prior to msgIndex.  The current status
	// in the vdb might not be the proceeding one if the vdb is stale.
	for j := 0; j <= msgIndex-1; j++ {
		if statusMsgs[j] != upgradeStatus {
			continue
		}
		errUpgrade := i.setUpgradeStatus(ctx, statusMsgs[msgIndex], sbName)
		upgradeStatus, err = i.Vdb.GetUpgradeStatus(sbName)
		if err != nil {
			errUpgrade = errors.Join(errUpgrade, err)
		}
		i.Log.Info("Status message after update", "msgIndex", msgIndex, "statusMsgs[msgIndex]", statusMsgs[msgIndex],
			"UpgradeStatus", upgradeStatus, "err", errUpgrade)
		return errUpgrade
	}
	return nil
}

// offlineUpgradeAllowed returns true if upgrade must be done offline
func offlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.OfflineUpgrade
}

// readOnlyOnlineUpgradeAllowed returns true if upgrade must be done online
// in read-only mode
func readOnlyOnlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.ReadOnlyOnlineUpgrade
}

// onlineUpgradeAllowed returns true if upgrade must be done with the
// online upgrade strategy.
func onlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return vdb.GetUpgradePolicyToUse() == vapi.OnlineUpgrade
}

// cachePrimaryImages will update o.PrimaryImages with the names of all of the primary images
func (i *UpgradeManager) cachePrimaryImages(ctx context.Context, sandbox string) error {
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting, sandbox)
	if err != nil {
		return err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]
		if i.isPrimary(sts.Labels) {
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
func (i *UpgradeManager) fetchOldImage(sandbox string) (string, bool) {
	targetImage, err := i.getTargetImage(sandbox)
	if err != nil {
		return "", false
	}
	for inx := range i.PrimaryImages {
		if i.PrimaryImages[inx] != targetImage {
			return i.PrimaryImages[inx], true
		}
	}
	return "", false
}

// getTargetImage returns the image that must be running
// in the main cluster or in a specific sandbox
func (i *UpgradeManager) getTargetImage(sandbox string) (string, error) {
	if sandbox == vapi.MainCluster {
		return i.Vdb.Spec.Image, nil
	}
	sb := i.Vdb.GetSandbox(sandbox)
	if sb == nil {
		return "", fmt.Errorf("could not find sandbox %q", sandbox)
	}
	// if the target cluster is a sandbox, the target image
	// is the one set for that specific sandbox
	if sb.Image == "" {
		return "", fmt.Errorf("could not find image for sandbox %q", sandbox)
	}
	return sb.Image, nil
}

// isPrimary returns true if the subcluster is primary
func (i *UpgradeManager) isPrimary(l map[string]string) bool {
	return l[vmeta.SubclusterTypeLabel] == vapi.PrimarySubcluster || l[vmeta.SubclusterTypeLabel] == vapi.SandboxPrimarySubcluster
}

func (i *UpgradeManager) traceActorReconcile(actor controllers.ReconcileActor) {
	i.Log.Info("starting actor for upgrade", "name", fmt.Sprintf("%T", actor))
}

// isSubclusterIdle will run a query to see the number of connections
// that are active for a given subcluster.  It returns a requeue error if there
// are still active connections.
func (i *UpgradeManager) isSubclusterIdle(ctx context.Context, pfacts *PodFacts, scName string) (ctrl.Result, error) {
	pf, ok := pfacts.findFirstUpPod(true, scName)
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
		i.Rec.Eventf(i.Vdb, corev1.EventTypeWarning, events.DrainSubclusterRetry,
			"Subcluster '%s' has active connections preventing the drain from succeeding", scName)
	}
	return res, nil
}

// closeAllSessions will run a query to close all active user sessions.
func (i *UpgradeManager) closeAllSessions(ctx context.Context, pfacts *PodFacts) error {
	pf, ok := pfacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.isPrimary && v.upNode
	})
	if !ok {
		i.Log.Info("No pod found to run vsql. Skipping close all sessions")
		return nil
	}

	sql := "select close_all_sessions();"
	cmd := []string{"-tAc", sql}
	_, _, err := pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return err
	}

	return nil
}

// createRestorePoint creates a restore point to backup the db in case upgrade does not go well.
func (i *UpgradeManager) createRestorePoint(ctx context.Context, pfacts *PodFacts, archive string) (string, ctrl.Result, error) {
	pf, ok := pfacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.isPrimary && v.upNode
	})
	if !ok {
		i.Log.Info("No pod found to run vsql. Requeueing for retrying creating restore point")
		return "", ctrl.Result{Requeue: true}, nil
	}

	sql := fmt.Sprintf("select count(*) from archives where name = '%s';", archive)
	cmd := []string{"-tAc", sql}
	stdout, _, err := pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return "", ctrl.Result{}, err
	}
	lines := strings.Split(stdout, "\n")
	arch, err := strconv.Atoi(lines[0])
	if err != nil {
		return "", ctrl.Result{}, err
	}
	// Create the archive only if it does not already exist
	clearKnob := "alter session set DisableNonReplicatableQueries = 0;"
	setKnob := "alter session clear DisableNonReplicatableQueries;"
	if arch == 0 {
		if pf.sandbox == vapi.MainCluster {
			sql = fmt.Sprintf("%s create archive %s; %s", clearKnob, archive, setKnob)
		} else {
			sql = fmt.Sprintf("create archive %s;", archive)
		}
		cmd = []string{"-tAc", sql}
		_, _, err = pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
		if err != nil {
			return "", ctrl.Result{}, err
		}
	}
	if pf.sandbox == vapi.MainCluster {
		sql = fmt.Sprintf("%s save restore point to archive %s; %s", clearKnob, archive, setKnob)
	} else {
		sql = fmt.Sprintf("save restore point to archive %s;", archive)
	}
	cmd = []string{"-tAc", sql}
	_, _, err = pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	return archive, ctrl.Result{}, err
}

// routeClientTraffic will update service objects for the source subcluster to
// route to the target subcluster
func (i *UpgradeManager) routeClientTraffic(ctx context.Context, pfacts *PodFacts, sc *vapi.Subcluster, selectors map[string]string) error {
	actor := MakeObjReconciler(i.Rec, i.Log, i.Vdb, pfacts, ObjReconcileModeAll)
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
	if err := i.Rec.GetClient().Get(ctx, svcName, svc); err != nil {
		if k8sErrors.IsNotFound(err) {
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

// logEventIfRequestedUpgradeIsDifferent will log an event if the requested
// upgrade, as per the upgrade policy, is different than the actual upgrade
// chosen.
func (i *UpgradeManager) logEventIfRequestedUpgradeIsDifferent(actualUpgrade vapi.UpgradePolicyType) {
	if !i.ContinuingUpgrade && i.Vdb.Spec.UpgradePolicy != actualUpgrade && i.Vdb.Spec.UpgradePolicy != vapi.AutoUpgrade {
		actualUpgradeAsText := strings.ToLower(string(actualUpgrade))
		i.Rec.Eventf(i.Vdb, corev1.EventTypeNormal, events.IncompatibleUpgradeRequested,
			"Requested upgrade is incompatible with the Vertica deployment. Falling back to %s upgrade.", actualUpgradeAsText)
	}
}

// getSubclusterNameFromSts returns the name of the subcluster from the given statefulset name
func (i *UpgradeManager) getSubclusterNameFromSts(ctx context.Context, stsName string) (string, error) {
	sts := appsv1.StatefulSet{}
	nm := names.GenNamespacedName(i.Vdb, stsName)
	err := i.Rec.GetClient().Get(ctx, nm, &sts)
	if err != nil {
		return "", fmt.Errorf("could not find statefulset %q: %w", stsName, err)
	}

	scNameFromLabel, ok := sts.Labels[vmeta.SubclusterNameLabel]
	if !ok {
		return "", fmt.Errorf("could not find subcluster name label %q in %q", vmeta.SubclusterNameLabel, stsName)
	}
	return scNameFromLabel, nil
}

func (i *UpgradeManager) checkAllSubscriptionsActive(ctx context.Context, pfacts *PodFacts) (ctrl.Result, error) {
	pf, ok := pfacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.isPrimary && v.upNode
	})
	if !ok {
		i.Log.Info("No pod found to run vsql. Requeueing for retrying checking subscription status")
		return ctrl.Result{Requeue: true}, nil
	}

	cmd := []string{"-tAc", "SELECT count(*) FROM node_subscriptions WHERE subscription_state != 'ACTIVE';"}
	stdout, _, err := pfacts.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return ctrl.Result{}, err
	}
	lines := strings.Split(stdout, "\n")
	inactive, err := strconv.Atoi(lines[0])
	if err != nil || inactive == 0 {
		return ctrl.Result{}, err
	}

	i.Log.Info("One or more node subscriptions is not active, requeueing")
	return ctrl.Result{Requeue: true}, nil
}
