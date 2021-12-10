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
	"strconv"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
)

// SubclusterHandle is a runtime object that has meta-data for a subcluster.  It
// holds everything in Subcluster plus additional state that isn't stored with
// the k8s meta-data.  The additional state forces this structure to exist only
// at runtime and is never stored in etcd.
type SubclusterHandle struct {
	vapi.Subcluster

	// Indicates whether this subcluster is a transient standby that is created
	// for online upgrade.
	IsStandby bool

	// The name of the image that is currently being run in this subcluster
	Image string
}

// makeSubclusterHandle will form a SubclusterHandle from a Subcluster object
// found in the VerticaDB
func makeSubclusterHandle(sc *vapi.Subcluster) *SubclusterHandle {
	return &SubclusterHandle{
		Subcluster: *sc,
		IsStandby:  false, // Assume not a standby since it is from VerticaDB
	}
}

// SPILLY - need to figure out how to see if the service object accepts traffic to this SubclusterHandle
// SPILLY - we are also adding the standby label to the service object.  That
// seems wrong since a service object will handle both kinds of traffic.
// Unless, we use that as an indication of what traffic is getting routed where.

// makeSubclusterHandleFromSts will form a SubclusterHandle from a StatefulSet
// object.
func makeSubclusterHandleFromSts(sts *appsv1.StatefulSet) *SubclusterHandle {
	sc := &SubclusterHandle{}
	sc.Name = sts.Labels[SubclusterNameLabel]
	sc.IsPrimary = sts.Labels[SubclusterTypeLabel] == PrimarySubclusterType
	sc.IsStandby, _ = strconv.ParseBool(sts.Labels[SubclusterStandbyLabel])
	sc.Image = sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image
	return sc
}
