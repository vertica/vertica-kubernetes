/*
Copyright [2021-2024] Open Text.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerticaReplicatorSpec defines the desired state of VerticaReplicator
type VerticaReplicatorSpec struct {

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Dummy placeholder field
	Foo string `json:"foo,omitempty"`
}

// VerticaReplicatorStatus defines the observed state of VerticaReplicator
type VerticaReplicatorStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Status message for replicator
	State string `json:"state,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Set of status conditions of replication process
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=verticareplicators,singular=verticareplicator,categories=all;vertica,shortName=vr
//+kubebuilder:subresource:status
//+operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1beta1,""}}

// VerticaReplicator is the Schema for the verticareplicators API
type VerticaReplicator struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaReplicatorSpec   `json:"spec,omitempty"`
	Status VerticaReplicatorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VerticaReplicatorList contains a list of VerticaReplicator
type VerticaReplicatorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaReplicator `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaReplicator{}, &VerticaReplicatorList{})
}
