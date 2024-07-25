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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type VerticaScrutinizeSpec struct {

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this VerticaScrutinize is defined for.  The
	// VerticaDB object must exist in the same namespace as this object.
	VerticaDBName string `json:"verticaDBName"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// This allows the user to select the volume to use for the final destination of the scrutinize tarball and
	// any intermediate files. The volume must have enough space to store the scrutinize data.
	// If this is omitted, then a simple emptyDir volume is created to store the scrutinize data.
	Volume *corev1.Volume `json:"volume,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// A list of additional init containers to run. These are run after running the init container that collects
	// the scrutinize command. These can be used to do some kind of post-processing of the tarball, such as uploading it
	// to some kind of storage.
	InitContainers []corev1.Container `json:"initContainers,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// In order to facilitate diagnosing less recent problems, scrutinize should be able to collect an arbitrary time range of logs.
	// With no options set, archived logs gathered for each node should be no older than 24 hours, and as recent as now.
	// With the oldest time param or log age set, no archives of vertica.log should be older than that time.
	// Timestamps should be formatted as: YYYY-MM-DD HH [+/-X], where [] is optional and +X represents X hours ahead of UTC.
	LogAgeOldestTime string `json:"logAgeOldestTime,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// In order to facilitate diagnosing less recent problems, scrutinize should be able to collect an arbitrary time range of logs.
	// With no options set, archived logs gathered for each node should be no older than 24 hours, and as recent as now.
	// With the newest time param set, no archives of vertica.log should be newer than that time.
	// Timestamps should be formatted as: YYYY-MM-DD HH [+/-X], where [] is optional and +X represents X hours ahead of UTC.
	LogAgeNewestTime string `json:"logAgeNewestTime,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// In order to facilitate diagnosing less recent problems, scrutinize should be able to collect an arbitrary time range of logs.
	// With no options set, archived logs gathered for each node should be no older than 24 hours, and as recent as now.
	// The hours param cannot be set alongside the Time options, and if attempted, should issue an error indicating so.
	LogAgeHours int `json:"logAgeHours,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// This defines the resource requests and limits for the scrutinize pod.
	// It is advisable that the request and limits match as this ensures the
	// pods are assigned to the guaranteed QoS class. This will reduces the
	// chance that pods are chosen by the OOM killer.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A map of label keys and values to restrict scrutinize node scheduling to workers
	// with matching labels.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Like nodeSelector this allows you to constrain the pod only to certain
	// pods. It is more expressive than just using node selectors.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity
	Affinity Affinity `json:"affinity,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// The priority class name given to the scrutinize. This affects
	// where the pod gets scheduled.
	// More info: https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Any tolerations and taints to use to aid in where to schedule a pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A list of annotations that will be added to the scrutinize pod.
	Annotations map[string]string `json:"annotations,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A list of labels that will be added to the scrutinize pod.
	Labels map[string]string `json:"labels,omitempty"`
}

// VerticaScrutinizeStatus defines the observed state of VerticaScrutinize
type VerticaScrutinizeStatus struct {

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// The name of the scrutinize pod that was created.
	PodName string `json:"podName"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// The UID of the pod name that was created.
	PodUID types.UID `json:"podUID"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// The name of the scrutinize tarball that was created.
	TarballName string `json:"tarballName"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// minimum timestamp for logs (default 24 hours ago)
	LogAgeOldestTime string `json:"logAgeOldestTime,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// maximum timestamp for logs (default none)
	LogAgeNewestTime string `json:"logAgeNewestTime,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// maximum timestamp for logs (default none)
	LogAgeHours int `json:"logAgeHours,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Status message for scrutinize
	State string `json:"state,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Set of status conditions to know how far along the scrutinize is.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

const (
	// ScrutinizePodCreated indicates whether the scrutinize pod has been created
	ScrutinizePodCreated = "ScrutinizePodCreated"
	// ScrutinizeCollectionFinished indicates whether scrutinize collection is done
	ScrutinizeCollectionFinished = "ScrutinizeCollectionFinished"
	// ScrutinizeReady indicates that there is a VerticaDB ready for scrutinize, meaning
	// the server version supports vclusterops and vclusterops is enabled
	ScrutinizeReady = "ScrutinizeReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=verticascrutinizers,singular=verticascrutinize,categories=all;vertica,shortName=vscr
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Pod",type="string",JSONPath=".status.podName"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{Pod,v1,""},{VerticaDB,vertica.com/v1beta1,""}}

// VerticaScrutinize is the schema for verticascrutinize API
type VerticaScrutinize struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaScrutinizeSpec   `json:"spec,omitempty"`
	Status VerticaScrutinizeStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VerticaScrutinizeList contains a list of VerticaScrutinize
type VerticaScrutinizeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaScrutinize `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaScrutinize{}, &VerticaScrutinizeList{})
}
