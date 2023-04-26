/*
Copyright [2021-2023] Micro Focus or one of its affiliates.

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
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EventTriggerSpec defines how to find objects that apply, what the match
// condition and a job template spec that gets created when a match occurs.
type EventTriggerSpec struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Objects that this event trigger will apply too.
	References []ETReference `json:"references"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// List of things that must be matched in order for the Job to be
	// created. Multiple matches are combined with AND logic.
	Matches []ETMatch `json:"matches"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// A template of a Job that will get created when the conditions are met for
	// any reference object.
	Template JobTemplate `json:"template"`
}

// ETReference is a way to identify an object or set of objects that will be
// watched.
type ETReference struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A single object, given by GVK + namespace + name, that this event trigger
	// will apply too.
	Object *ETRefObject `json:"object,omitempty"`
}

// ETRefObject is a way to indentify a single object by GVK and name in the
// spec portion of the CR. This matches objects in the same namespace as the CR.
type ETRefObject struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The API version of the reference object
	APIVersion string `json:"apiVersion"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The kind of the reference object
	Kind string `json:"kind"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The namespace that the reference object exists in.
	Namespace string `json:"namespace,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The name of the reference object. This doesn't have to exist prior to
	// creating the CR.
	Name string `json:"name"`
}

// ETMatch defines a condition to match that will trigger job creation.
type ETMatch struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Details about a status condition that must match.
	Condition *ETCondition `json:"condition,omitempty"`
}

// ETCondition is used to match on a specific value of a status condition.
type ETCondition struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The name of the status condition to check.
	Type string `json:"type"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The expected value of the status condition when a match occurs.
	Status corev1.ConditionStatus `json:"status"`
}

// JobTemplate defines the template to use to construct the Job object. This is
// used when the event matches in an object reference.
type JobTemplate struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// The job's object meta data. At a minimum, the name or generateName must
	// be set.
	Metadata JobObjectMeta `json:"metadata"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:required
	// +kubebuilder:validation:Schemaless
	// +kubebuilder:pruning:PreserveUnknownFields
	// Specification of the desired behavior of the job.
	Spec batchv1.JobSpec `json:"spec"`
}

// JobObjectMeta is meta-data of the Job object that the operator constructs.
type JobObjectMeta struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// Name must be unique within a namespace. Can be omitted if GenerateName is provided.
	Name string `json:"name"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// GenerateName is an optional prefix, used by the server, to generate a unique
	// name ONLY IF the Name field has not been provided.
	GenerateName string `json:"generateName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// Map of string keys and values that can be used to organize and categorize
	// (scope and select) objects.
	Labels map[string]string `json:"labels,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// Annotations is an unstructured key value map stored with a resource that may be
	// set by external tools to store and retrieve arbitrary metadata. They are not
	// queryable and should be preserved when modifying objects.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// EventTriggerStatus defines the observed state of EventTrigger
type EventTriggerStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Status about each of the reference objects
	References []ETRefObjectStatus `json:"references"`
}

// ETRefObjectStatus provides status information about a single reference object
type ETRefObjectStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +kubebuilder:validation:required
	// The API version of the reference object
	APIVersion string `json:"apiVersion"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +kubebuilder:validation:required
	// The kind of the reference object
	Kind string `json:"kind"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The namespace that the reference object exists in.
	Namespace string `json:"namespace,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +kubebuilder:validation:required
	// The name of the reference object. This doesn't have to exist prior to
	// creating the CR.
	Name string `json:"name"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The uid of the reference object
	UID types.UID `json:"uid"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The last known resource version of the reference object
	ResourceVersion string `json:"resourceVersion"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// If a job was created because a match was found for this reference object,
	// this is the namespace the job is found in. This pairs with the jobName
	// parameter to uniquely identify the job.
	JobNamespace string `json:"jobNamespace"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// If a job was created because a match was found for this reference object,
	// this is the name of that job. This pairs with the jobNamespace parameter
	// to uniquely identify the job.
	JobName string `json:"jobName"`
}

// IsSameObject will compare two ETRefObjectStatus objects and return true if they
// are both referencing the same k8s object.
func (r *ETRefObjectStatus) IsSameObject(other *ETRefObjectStatus) bool {
	return r.APIVersion == other.APIVersion && r.Kind == other.Kind && r.Namespace == other.Namespace && r.Name == other.Name
}

//+kubebuilder:object:root=true
//+kubebuilder:resource:categories=vertica,shortName=et
//+kubebuilder:subresource:status
// +operator-sdk:csv:customresourcedefinitions:resources={{Job,batch/v1,""}}

// EventTrigger is the Schema for the eventtriggers API
type EventTrigger struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   EventTriggerSpec   `json:"spec,omitempty"`
	Status EventTriggerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// EventTriggerList contains a list of EventTrigger
type EventTriggerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []EventTrigger `json:"items"`
}

func init() {
	SchemeBuilder.Register(&EventTrigger{}, &EventTriggerList{})
}

func (e *EventTrigger) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      e.ObjectMeta.Name,
		Namespace: e.ObjectMeta.Namespace,
	}
}

func makeSampleETName() types.NamespacedName {
	return types.NamespacedName{Name: "et-sample", Namespace: "default"}
}

// MakeET will make an EventTrigger for test purposes
func MakeET() *EventTrigger {
	defVDBName := MakeVDBName()
	nm := makeSampleETName()
	return &EventTrigger{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       EventTriggerKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
			UID:       "abcdef-ghi-jk",
		},
		Spec: EventTriggerSpec{
			References: []ETReference{
				{Object: &ETRefObject{
					APIVersion: GroupVersion.String(),
					Kind:       VerticaDBKind,
					Name:       defVDBName.Name,
				}},
			},
			Matches: []ETMatch{
				{Condition: &ETCondition{
					Type:   string(DBInitialized),
					Status: corev1.ConditionTrue,
				}},
			},
			Template: JobTemplate{
				Metadata: JobObjectMeta{
					Name: "job1",
				},
				Spec: batchv1.JobSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							RestartPolicy: "OnFailure",
							Containers: []corev1.Container{
								{Name: "main", Image: "run-me"},
							},
						},
					},
				},
			},
		},
	}
}
