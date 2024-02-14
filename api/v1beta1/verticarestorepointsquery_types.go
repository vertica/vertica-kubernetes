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
	"github.com/vertica/vcluster/vclusterops"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerticaRestorePointsQuerySpec defines the desired state of VerticaRestorePointsQuery
type VerticaRestorePointsQuerySpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this VerticaRestorePointsQuery is defined for.  The
	// VerticaDB object must exist in the same namespace as this object.
	VerticaDBName string `json:"verticaDBName"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional parameter that will limit the query to only restore points
	// from this archive
	ArchiveName string `json:"archiveName"`
}

const (
	archiveNm = "backup" // constants for test purposes
)

// VerticaRestorePointsQueryStatus defines the observed state of VerticaRestorePointsQuery
type VerticaRestorePointsQueryStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Conditions for VerticaRestorePointsQuery
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Status message for running query
	State string `json:"state,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// This contains the result of the restore points query. Check the QueryComplete
	// status condition to know when this has been populated by the operator.
	RestorePoints []vclusterops.RestorePoint `json:"restorePoints,omitempty"`
}

const (
	// Querying indicates whether the operator should query for list restore points
	Querying = "Querying"
	// QueryComplete indicates the query has been completed
	QueryComplete = "QueryComplete"
	// QueryReady indicates whether the operator is ready to start querying
	QueryReady = "QueryReady"
)

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=vertica,shortName=vrpq
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="VerticaDB",type="string",JSONPath=".spec.verticaDBName"
// +kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1beta1,""}}

// VerticaRestorePointsQuery is the Schema for the verticarestorepointsqueries API
type VerticaRestorePointsQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaRestorePointsQuerySpec   `json:"spec,omitempty"`
	Status VerticaRestorePointsQueryStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VerticaRestorePointsQueryList contains a list of VerticaRestorePointsQuery
type VerticaRestorePointsQueryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaRestorePointsQuery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaRestorePointsQuery{}, &VerticaRestorePointsQueryList{})
}
