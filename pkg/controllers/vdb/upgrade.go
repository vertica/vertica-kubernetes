/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type UpgradeManager struct {
	VRec              *VerticaDBReconciler
	Vdb               *vapi.VerticaDB
	Log               logr.Logger
	Finder            iter.SubclusterFinder
	ContinuingUpgrade bool // true if UpdateInProgress was already set upon entry
	StatusCondition   vapi.VerticaDBConditionType
	// Function that will check if the image policy allows for a type of upgrade (offline or online)
	IsAllowedForUpgradePolicyFunc func(vdb *vapi.VerticaDB) bool
}

// MakeUpgradeManager will construct a UpgradeManager object
func MakeUpgradeManager(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	statusCondition vapi.VerticaDBConditionType,
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

	if ok, err := i.isUpgradeInProgress(); ok || err != nil {
		return ok, err
	}

	if ok := i.IsAllowedForUpgradePolicyFunc(i.Vdb); !ok {
		return ok, nil
	}

	return i.isVDBImageDifferent(ctx)
}

// isUpgradeInProgress returns true if state indicates that an upgrade
// is already occurring.
func (i *UpgradeManager) isUpgradeInProgress() (bool, error) {
	// We first check if the status condition indicates the upgrade is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[i.StatusCondition]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", i.StatusCondition)
	}
	if inx < len(i.Vdb.Status.Conditions) && i.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		// Set a flag to indicate that we are continuing an upgrade.  This silences the UpgradeStarted event.
		i.ContinuingUpgrade = true
		return true, nil
	}
	return false, nil
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
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != i.Vdb.Spec.Image {
			return true, nil
		}
	}

	return false, nil
}

// startUpgrade handles condition status and event recording for start of an upgrade
func (i *UpgradeManager) startUpgrade(ctx context.Context) (ctrl.Result, error) {
	i.Log.Info("Starting upgrade for reconciliation iteration", "ContinuingUpgrade", i.ContinuingUpgrade,
		"New Image", i.Vdb.Spec.Image)
	if err := i.toggleImageChangeInProgress(ctx, corev1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message the first time we begin an upgrade.
	if !i.ContinuingUpgrade {
		i.VRec.EVRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeStart,
			"Vertica server upgrade has started.")
	}
	return ctrl.Result{}, nil
}

// finishUpgrade handles condition status and event recording for the end of an upgrade
func (i *UpgradeManager) finishUpgrade(ctx context.Context) (ctrl.Result, error) {
	if err := i.setUpgradeStatus(ctx, ""); err != nil {
		return ctrl.Result{}, err
	}

	if err := i.toggleImageChangeInProgress(ctx, corev1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	i.Log.Info("The upgrade has completed successfully")
	i.VRec.EVRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.UpgradeSucceeded,
		"Vertica server upgrade has completed successfully.  New image is '%s'", i.Vdb.Spec.Image)

	return ctrl.Result{}, nil
}

// toggleImageChangeInProgress is a helper for updating the
// ImageChangeInProgress condition's.  We set the ImageChangeInProgress plus the
// one defined in i.StatusCondition.
func (i *UpgradeManager) toggleImageChangeInProgress(ctx context.Context, newVal corev1.ConditionStatus) error {
	err := vdbstatus.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.VerticaDBCondition{Type: vapi.ImageChangeInProgress, Status: newVal},
	)
	if err != nil {
		return err
	}
	return vdbstatus.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.VerticaDBCondition{Type: i.StatusCondition, Status: newVal},
	)
}

// setUpgradeStatus is a helper to set the upgradeStatus message.
func (i *UpgradeManager) setUpgradeStatus(ctx context.Context, msg string) error {
	return vdbstatus.UpdateUpgradeStatus(ctx, i.VRec.Client, i.Vdb, msg)
}

// updateImageInStatefulSets will change the image in each of the statefulsets.
// This changes the images in all subclusters except any transient ones.
func (i *UpgradeManager) updateImageInStatefulSets(ctx context.Context) (int, ctrl.Result, error) {
	numStsChanged := 0 // Count to keep track of the nubmer of statefulsets updated

	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the upgrade was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the upgrade.
	stss, err := i.Finder.FindStatefulSets(ctx, iter.FindExisting)
	if err != nil {
		return numStsChanged, ctrl.Result{}, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		isTransient, err := strconv.ParseBool(sts.Labels[builder.SubclusterTransientLabel])
		if err != nil {
			return numStsChanged, ctrl.Result{}, err
		}
		if isTransient {
			continue
		}

		if stsUpdated, err := i.updateImageInStatefulSet(ctx, sts); err != nil {
			return numStsChanged, ctrl.Result{}, err
		} else if stsUpdated {
			numStsChanged++
		}
	}
	return numStsChanged, ctrl.Result{}, nil
}

// updateImageInStatefulSet will update the image in the given statefulset.  It
// returns true if the image was changed.
func (i *UpgradeManager) updateImageInStatefulSet(ctx context.Context, sts *appsv1.StatefulSet) (bool, error) {
	stsUpdated := false
	// Skip the statefulset if it already has the proper image.
	if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != i.Vdb.Spec.Image {
		i.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
		sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image = i.Vdb.Spec.Image
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
	pods, err := i.Finder.FindPods(ctx, iter.FindExisting)
	if err != nil {
		return numPodsDeleted, err
	}
	for inx := range pods.Items {
		pod := &pods.Items[inx]

		// If scName was passed in, we only delete for a specific subcluster
		if scName != "" {
			scNameFromLabel, ok := pod.Labels[builder.SubclusterNameLabel]
			if ok && scNameFromLabel != scName {
				continue
			}
		}

		// Skip the pod if it already has the proper image.
		if pod.Spec.Containers[names.ServerContainerIndex].Image != i.Vdb.Spec.Image {
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

// onlineUpgradeAllowed returns true if upgrade must be done online
func onlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	if vdb.Spec.UpgradePolicy == vapi.OfflineUpgrade {
		return false
	}
	// If the field value is missing, we treat it as if Auto was selected.
	if vdb.Spec.UpgradePolicy == vapi.AutoUpgrade || vdb.Spec.UpgradePolicy == "" {
		// Online upgrade with a transient subcluster works by scaling out new
		// subclusters to handle the primaries as they come up with the new
		// versions.  If we don't have a license, it isn't going to work.
		if (vdb.RequiresTransientSubcluster() && vdb.Spec.LicenseSecret == "") || vdb.Spec.KSafety == vapi.KSafety0 {
			return false
		}
	}
	// Online upgrade can only be done if we are already on a server version
	// that supports it.  It we are on an older version, we will fallback to
	// offline even though online may have been specified in the vdb.
	vinf, ok := version.MakeInfoFromVdb(vdb)
	if ok && vinf.IsEqualOrNewer(version.OnlineUpgradeVersion) {
		return true
	}
	return false
}

// offlineUpgradeAllowed returns true if upgrade must be done offline
func offlineUpgradeAllowed(vdb *vapi.VerticaDB) bool {
	return !onlineUpgradeAllowed(vdb)
}
