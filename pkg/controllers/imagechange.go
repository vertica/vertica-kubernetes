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
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/status"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ImageChangeManager struct {
	VRec                  *VerticaDBReconciler
	Vdb                   *vapi.VerticaDB
	Log                   logr.Logger
	Finder                SubclusterFinder
	ContinuingImageChange bool // true if UpdateInProgress was already set upon entry
	StatusCondition       vapi.VerticaDBConditionType
	// Function that will check if the image policy allows for a type of upgrade (offline or online)
	IsAllowedForImageChangePolicyFunc func(vdb *vapi.VerticaDB) bool
}

// MakeImageChangeManager will construct a ImageChangeManager object
func MakeImageChangeManager(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	statusCondition vapi.VerticaDBConditionType,
	isAllowedForImageChangePolicyFunc func(vdb *vapi.VerticaDB) bool) *ImageChangeManager {
	return &ImageChangeManager{
		VRec:                              vdbrecon,
		Vdb:                               vdb,
		Log:                               log,
		Finder:                            MakeSubclusterFinder(vdbrecon.Client, vdb),
		StatusCondition:                   statusCondition,
		IsAllowedForImageChangePolicyFunc: isAllowedForImageChangePolicyFunc,
	}
}

// IsImageChangeNeeded checks whether an image change is needed and/or in
// progress.  It will return true for the first parm if an image change should
// proceed.
func (i *ImageChangeManager) IsImageChangeNeeded(ctx context.Context) (bool, error) {
	// no-op for ScheduleOnly init policy
	if i.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return false, nil
	}

	if ok, err := i.isImageChangeInProgress(); ok || err != nil {
		return ok, err
	}

	if ok := i.IsAllowedForImageChangePolicyFunc(i.Vdb); !ok {
		return ok, nil
	}

	return i.isVDBImageDifferent(ctx)
}

// isImageChangeInProgress returns true if state indicates that an image change
// is already occurring.
func (i *ImageChangeManager) isImageChangeInProgress() (bool, error) {
	// We first check if the status condition indicates the image change is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[i.StatusCondition]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", i.StatusCondition)
	}
	if inx < len(i.Vdb.Status.Conditions) && i.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		// Set a flag to indicate that we are continuing an image change.  This silences the ImageChangeStarted event.
		i.ContinuingImageChange = true
		return true, nil
	}
	return false, nil
}

// isVDBImageDifferent will check if an image change is needed based on the
// image being different between the Vdb and any of the statefulset's.
func (i *ImageChangeManager) isVDBImageDifferent(ctx context.Context) (bool, error) {
	stss, err := i.Finder.FindStatefulSets(ctx, FindInVdb)
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

// startImageChange handles condition status and event recording for start of an image change
func (i *ImageChangeManager) startImageChange(ctx context.Context) (ctrl.Result, error) {
	i.Log.Info("Starting image change for reconciliation iteration", "ContinuingImageChange", i.ContinuingImageChange)
	if err := i.toggleImageChangeInProgress(ctx, corev1.ConditionTrue); err != nil {
		return ctrl.Result{}, err
	}

	// We only log an event message the first time we begin an image change.
	if !i.ContinuingImageChange {
		i.VRec.EVRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.ImageChangeStart,
			"Vertica server image change has started.  New image is '%s'", i.Vdb.Spec.Image)
	}
	return ctrl.Result{}, nil
}

// finishImageChange handles condition status and event recording for the end of an image change
func (i *ImageChangeManager) finishImageChange(ctx context.Context) (ctrl.Result, error) {
	if err := i.setImageChangeStatus(ctx, ""); err != nil {
		return ctrl.Result{}, err
	}

	if err := i.toggleImageChangeInProgress(ctx, corev1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}

	i.VRec.EVRec.Eventf(i.Vdb, corev1.EventTypeNormal, events.ImageChangeSucceeded,
		"Vertica server image change has completed successfully")

	return ctrl.Result{}, nil
}

// toggleImageChangeInProgress is a helper for updating the
// ImageChangeInProgress condition's.  We set the ImageChangeInProgress plus the
// one defined in i.StatusCondition.
func (i *ImageChangeManager) toggleImageChangeInProgress(ctx context.Context, newVal corev1.ConditionStatus) error {
	err := status.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.VerticaDBCondition{Type: vapi.ImageChangeInProgress, Status: newVal},
	)
	if err != nil {
		return err
	}
	return status.UpdateCondition(ctx, i.VRec.Client, i.Vdb,
		vapi.VerticaDBCondition{Type: i.StatusCondition, Status: newVal},
	)
}

// setImageChangeStatus is a helper to set the imageChangeStatus message.
func (i *ImageChangeManager) setImageChangeStatus(ctx context.Context, msg string) error {
	return status.UpdateImageChangeStatus(ctx, i.VRec.Client, i.Vdb, msg)
}

// updateImageInStatefulSets will change the image in each of the statefulsets.
// Caller can indicate whether primary or secondary types change.
func (i *ImageChangeManager) updateImageInStatefulSets(ctx context.Context, chgPrimary, chgSecondary bool) (int, ctrl.Result, error) {
	numStsChanged := 0 // Count to keep track of the nubmer of statefulsets updated

	// We use FindExisting for the finder because we only want to work with sts
	// that already exist.  This is necessary incase the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	stss, err := i.Finder.FindStatefulSets(ctx, FindExisting)
	if err != nil {
		return numStsChanged, ctrl.Result{}, err
	}
	for inx := range stss.Items {
		sts := &stss.Items[inx]

		if !chgPrimary && sts.Labels[SubclusterTypeLabel] == PrimarySubclusterType {
			continue
		}
		if !chgSecondary && sts.Labels[SubclusterTypeLabel] == SecondarySubclusterType {
			continue
		}
		if sts.Labels[SubclusterTypeLabel] == StandbySubclusterType {
			continue
		}

		// Skip the statefulset if it already has the proper image.
		if sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image != i.Vdb.Spec.Image {
			i.Log.Info("Updating image in old statefulset", "name", sts.ObjectMeta.Name)
			err = i.setImageChangeStatus(ctx, "Rescheduling pods with new image name")
			if err != nil {
				return numStsChanged, ctrl.Result{}, err
			}
			sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image = i.Vdb.Spec.Image
			// We change the update strategy to OnDelete.  We don't want the k8s
			// sts controller to interphere and do a rolling update after the
			// update has completed.  We don't explicitly change this back.  The
			// ObjReconciler will handle it for us.
			sts.Spec.UpdateStrategy.Type = appsv1.OnDeleteStatefulSetStrategyType
			err = i.VRec.Client.Update(ctx, sts)
			if err != nil {
				return numStsChanged, ctrl.Result{}, err
			}
			numStsChanged++
		}
	}
	return numStsChanged, ctrl.Result{}, nil
}

// deletePodsRunningOldImage will delete pods that have the old image.  It will return the
// number of pods that were deleted.  Callers can control whether to delete pods
// just for the primary or primary/secondary.
func (i *ImageChangeManager) deletePodsRunningOldImage(ctx context.Context, delSecondary bool) (int, ctrl.Result, error) {
	numPodsDeleted := 0 // Tracks the number of pods that were deleted

	// We use FindExisting for the finder because we only want to work with pods
	// that already exist.  This is necessary in case the image change was paired
	// with a scaling operation.  The pod change due to the scaling operation
	// doesn't take affect until after the image change.
	pods, err := i.Finder.FindPods(ctx, FindExisting)
	if err != nil {
		return numPodsDeleted, ctrl.Result{}, err
	}
	for inx := range pods.Items {
		pod := &pods.Items[inx]

		// We aren't deleting secondary pods, so we only continue if the pod is
		// for a primary
		if !delSecondary {
			scType, ok := pod.Labels[SubclusterTypeLabel]
			if ok && scType != vapi.PrimarySubclusterType {
				continue
			}
		}

		// Skip the pod if it already has the proper image.
		if pod.Spec.Containers[names.ServerContainerIndex].Image != i.Vdb.Spec.Image {
			i.Log.Info("Deleting pod that had old image", "name", pod.ObjectMeta.Name)
			err = i.VRec.Client.Delete(ctx, pod)
			if err != nil {
				return numPodsDeleted, ctrl.Result{}, err
			}
			numPodsDeleted++
		}
	}
	return numPodsDeleted, ctrl.Result{}, nil
}

// onlineImageChangeAllowed returns true if image change must be done online
func onlineImageChangeAllowed(vdb *vapi.VerticaDB) bool {
	if vdb.Spec.ImageChangePolicy == vapi.OfflineImageChange {
		return false
	}
	// If the field value is missing, we treat it as if Auto was selected.
	if vdb.Spec.ImageChangePolicy == vapi.AutoImageChange || vdb.Spec.ImageChangePolicy == "" {
		// Online image change works by scaling out new subclusters to handle
		// the primaries as they come up with the new versions.  If we don't
		// have a license, it isn't going to work.
		if vdb.Spec.LicenseSecret == "" || vdb.Spec.KSafety == vapi.KSafety0 {
			return false
		}
		vinf, ok := version.MakeInfo(vdb)
		if ok && vinf.IsEqualOrNewer(version.OnlineImageChangeVersion) {
			return true
		}
		return false
	}
	return true
}

// offlineImageChangeAllowed returns true if image change must be done offline
func offlineImageChangeAllowed(vdb *vapi.VerticaDB) bool {
	return !onlineImageChangeAllowed(vdb)
}
