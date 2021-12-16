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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

// SubclusterHandle is a runtime object that has meta-data for a subcluster.  It
// holds everything in Subcluster plus additional state that isn't stored with
// the k8s meta-data.  The additional state forces this structure to exist only
// at runtime and is never stored in etcd.
type SubclusterHandle struct {
	vapi.Subcluster

	// The name of the image that is currently being run in this subcluster.  If
	// the corresponding sts doesn't exist, then this will be left blank.
	Image string

	// Indicates whether a service object is currently routing traffic to this
	// subcluster.  When dealing with primary and standby subclusters pairs,
	// only one will have traffic routed to it.
	IsAcceptingTraffic bool
}

const (
	PrimarySubclusterType   = "primary"
	StandbySubclusterType   = "standby"
	SecondarySubclusterType = "secondary"
)

// GetSubclusterType returns the type of the subcluster in string form
func (s *SubclusterHandle) GetSubclusterType() string {
	if s.IsPrimary {
		if s.IsStandby {
			return StandbySubclusterType
		}
		return PrimarySubclusterType
	}
	return SecondarySubclusterType
}

func (s *SubclusterHandle) SetIsAcceptingTraffic(svcLabels map[string]string) error {
	// We determine if traffic is routed to the subcluster by checking the
	// labels from the service.
	s.IsAcceptingTraffic = svcLabels[SubclusterTypeLabel] == s.GetSubclusterType()
	return nil
}

// makeSubclusterHandleFromSts will form a SubclusterHandle from a StatefulSet
// object.
func makeSubclusterHandleFromSts(sts *appsv1.StatefulSet, svcMap map[string]corev1.Service) *SubclusterHandle {
	sc := &SubclusterHandle{}
	sc.Name = sts.Labels[SubclusterNameLabel]
	sc.IsPrimary = sts.Labels[SubclusterTypeLabel] == PrimarySubclusterType
	sc.IsStandby = sts.Labels[SubclusterTypeLabel] == StandbySubclusterType
	sc.Image = sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image

	// We check the service object to see if traffic is currently being routed
	// to the subcluster.
	svc, ok := svcMap[sc.Name]
	if ok {
		sc.IsAcceptingTraffic = svc.Spec.Selector[SubclusterTypeLabel] == sc.GetSubclusterType()
	}

	return sc
}
