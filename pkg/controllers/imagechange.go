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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
)

type ImageChangeReconciler interface {
	IsAllowedForImageChangePolicy(vdb *vapi.VerticaDB) bool
	SetContinuingImageChange()
}

type ImageChangeInitiator struct {
	Reconciler ImageChangeReconciler
	Vdb        *vapi.VerticaDB
	Finder     SubclusterFinder
}

// MakeImageChangeInitiator will construct a ImageChangeInitiator object
func MakeImageChangeInitiator(vdbrecon *VerticaDBReconciler, vdb *vapi.VerticaDB,
	reconciler ImageChangeReconciler) *ImageChangeInitiator {
	return &ImageChangeInitiator{
		Reconciler: reconciler,
		Vdb:        vdb,
		Finder:     MakeSubclusterFinder(vdbrecon.Client, vdb),
	}
}

// IsImageChangeNeeded checks whether an image change is needed and/or in
// progress.  It will return true for the first parm is an image change should
// proceed.
func (i *ImageChangeInitiator) IsImageChangeNeeded(ctx context.Context) (bool, error) {
	// no-op for ScheduleOnly init policy
	if i.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return false, nil
	}

	if ok, err := i.isImageChangeInProgress(); ok || err != nil {
		return ok, err
	}

	if ok := i.Reconciler.IsAllowedForImageChangePolicy(i.Vdb); !ok {
		return ok, nil
	}

	return i.isVDBImageDifferent(ctx)
}

// isImageChangeInProgress returns true if state indicates that an image change
// is already occurring.
func (i *ImageChangeInitiator) isImageChangeInProgress() (bool, error) {
	// We first check if the status condition indicates the image change is in progress
	inx, ok := vapi.VerticaDBConditionIndexMap[vapi.ImageChangeInProgress]
	if !ok {
		return false, fmt.Errorf("verticaDB condition '%s' missing from VerticaDBConditionType", vapi.ImageChangeInProgress)
	}
	if inx < len(i.Vdb.Status.Conditions) && i.Vdb.Status.Conditions[inx].Status == corev1.ConditionTrue {
		// Set a flag to indicate that we are continuing an image change.  This silences the ImageChangeStarted event.
		i.Reconciler.SetContinuingImageChange()
		return true, nil
	}
	return false, nil
}

func (i *ImageChangeInitiator) isVDBImageDifferent(ctx context.Context) (bool, error) {
	// Next check if an image change is needed based on the image being different
	// between the Vdb and any of the statefulset's.
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
