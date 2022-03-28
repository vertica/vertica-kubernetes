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

// nolint:lll
package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// VerticaAutoscalerSpec defines the desired state of VerticaAutoscaler
type VerticaAutoscalerSpec struct {
	// Important: Run "make" to regenerate code after modifying this file

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this autoscaler is defined for.  The
	// VerticaDB object must exist in the same namespaec as this object.
	VerticaDBName string `json:"verticaDBName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:default:="Pod"
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Create","urn:alm:descriptor:com.tectonic.ui:select:Pod","urn:alm:descriptor:com.tectonic.ui:select:Subcluster"}
	// This defines how the scaling will happen.  This can be one of the following:
	// - Pod: Only increase or decrease the size of an existing subcluster.
	//   This cannot be used if more than one subcluster is selected with
	//   subclusterServiceName.
	// - Subcluster: Scaling will be achieved by creating or deleting entire subclusters.
	//   New subclusters are created using subclusterTemplate as a template.
	//   Sizes of existing subclusters will remain the same.
	ScalingGranularity ScalingGranularityType `json:"scalingGranularity"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// This acts as a selector for the subclusters that being scaled together.
	// The name refers to the service name as defined in the subcluster section
	// of the VerticaDB, which is typically the same name as the subcluster name.
	SubclusterServiceName string `json:"subclusterServiceName"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// When the scaling granularity is Subcluster, this field defines a template
	// to use for when a new subcluster needs to be created.  The service name
	// must match the subclusterServiceName parameter.  The name of the
	// subcluster will be auto generated when the subcluster is added to the
	// VerticaDB.
	Template Subcluster `json:"template"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount"
	// This is the total pod count for all subclusters that match the
	// subclusterServiceName.  Changing this value may trigger a change in the
	// VerticaDB that is associated with this object.  This value is generally
	// left as the default and modified by the horizontal autoscaler through the
	// /scale subresource.
	TargetSize int32 `json:"targetSize,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// If true, this will cause the operator to scale down to zero pods if the
	// targetSize is zero.  If scaling by subcluster this will remove all
	// subclusters that match the service name.  If false, the operator will
	// ignore a targetSize of zero.
	AllowScaleToZero bool `json:"allowScaleToZero,omitempty"`
}

type ScalingGranularityType string

const (
	PodScalingGranularity        = "Pod"
	SubclusterScalingGranularity = "Subcluster"
)

// VerticaAutoscalerStatus defines the observed state of VerticaAutoscaler
type VerticaAutoscalerStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The total number of times the operator has scaled up/down the VerticaDB.
	ScalingCount int `json:"scalingCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The selector used to find all of the pods for this autoscaler.
	Selector string `json:"selector"`
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:shortName=vas
//+kubebuilder:subresource:status
//+kubebuilder:subresource:scale:specpath=.spec.targetSize,statuspath=.status.scalingCount,selectorpath=.status.selector
//+kubebuilder:printcolumn:name="Scaling Count",type="integer",JSONPath=".status.scalingCount"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1beta1,""}}

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

// MakeVASName is a helper that creates a sample name for test purposes
func MakeVASName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-vas-sample", Namespace: "default"}
}

// MakeVAS is a helper that constructs a fully formed VerticaAutoscaler struct using the sample name.
// This is intended for test purposes.
func MakeVAS() *VerticaAutoscaler {
	vasNm := MakeVASName()
	vdbNm := MakeVDBName()
	return &VerticaAutoscaler{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "vertica.com/v1beta1",
			Kind:       "VerticaAutoscaler",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        vasNm.Name,
			Namespace:   vasNm.Namespace,
			UID:         "abcdef-ghi",
			Annotations: make(map[string]string),
		},
		Spec: VerticaAutoscalerSpec{
			VerticaDBName:         vdbNm.Name,
			ScalingGranularity:    "Pod",
			SubclusterServiceName: "sc1",
		},
	}
}

// IsScalingAllowed returns true if scaling should proceed based on the targetSize
func (v *VerticaAutoscaler) IsScalingAllowed() bool {
	return v.Spec.TargetSize > 0 || v.Spec.AllowScaleToZero
}
