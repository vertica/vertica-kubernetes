/*
Copyright [2021-2022] Micro Focus or one of its affiliates.

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

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VerticaAutoscalerSpec defines the desired state of VerticaAutoscaler
type VerticaAutoscalerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this autoscaler is defined for.  The
	// VerticaDB object must exist in the same namespaec as this object.
	VerticaDBName string `json:"vdbName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:default:="Pod"
	// +kubebuilder:validation:Optional
	// This defines how the scaling will happen.  This can be one of the following:
	// - Pod: Only increase or decrease the size of an existing subcluster.
	//   This cannot be used if more than one subcluster is defined in subclusters.
	// - Subcluster: Scaling will be achieved by creating or deleting entire subclusters.
	//   New subclusters are created using subclusterTemplate as a template.
	//   Sizes of existing subclusters will remain the same.
	ScalingGranularity ScalingGranularityType `json:"scalingGranularity"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A list of subclusters, as defined in the VerticaDB, that this
	// autoscaler will manage.  If scalingGranularity of Subcluster, this is
	// also where you define the template of new subclusters that the autoscaler
	// may create.
	Subclusters SubclusterSelection `json:"subclusters"`
}

type ScalingGranularityType string

const (
	PodScalingGranularity        = "Pod"
	SubclusterScalingGranularity = "Subcluster"
)

// VerticaAutoscalerStatus defines the observed state of VerticaAutoscaler
type VerticaAutoscalerStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:shortName=vas
//+kubebuilder:subresource:status

// VerticaAutoscaler is the Schema for the verticaautoscalers API
type VerticaAutoscaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaAutoscalerSpec   `json:"spec,omitempty"`
	Status VerticaAutoscalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VerticaAutoscalerList contains a list of VerticaAutoscaler
type VerticaAutoscalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaAutoscaler `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaAutoscaler{}, &VerticaAutoscalerList{})
}
