/*
Copyright [2021-2023] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
)

// Affinity is used instead of corev1.Affinity and behaves the same.
// This structure is used in some CRs fields to define the "Affinity".
// corev1.Affinity is composed of 3 fields and for each of them,
// there is a x-descriptor. However there is not a x-descriptor for corev1.Affinity itself.
// In this structure, we have the same fields as corev1' but we also added
// the corresponding x-descriptor to each field. That will be useful for the Openshift web console.
type Affinity struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:nodeAffinity"
	// Describes node affinity scheduling rules for the pod.
	// +optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty" protobuf:"bytes,1,opt,name=nodeAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAffinity"
	// Describes pod affinity scheduling rules (e.g. co-locate this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAffinity *corev1.PodAffinity `json:"podAffinity,omitempty" protobuf:"bytes,2,opt,name=podAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAntiAffinity"
	// Describes pod anti-affinity scheduling rules (e.g. avoid putting this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty" protobuf:"bytes,3,opt,name=podAntiAffinity"`
}

// IsStatusConditionTrue returns true when the conditionType is present and set to
// `metav1.ConditionTrue`
func (vscr *VerticaScrutinize) IsStatusConditionTrue(statusCondition string) bool {
	return meta.IsStatusConditionTrue(vscr.Status.Conditions, statusCondition)
}

// IsStatusConditionFalse returns true when the conditionType is present and set to
// `metav1.ConditionFalse`
func (vscr *VerticaScrutinize) IsStatusConditionFalse(statusCondition string) bool {
	return meta.IsStatusConditionFalse(vscr.Status.Conditions, statusCondition)
}
