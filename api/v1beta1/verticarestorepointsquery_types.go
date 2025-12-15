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
//nolint:lll
package v1beta1

import (
	"github.com/vertica/vcluster/vclusterops"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// VerticaRestorePointsQuerySpec defines the desired state of VerticaRestorePointsQuery
type VerticaRestorePointsQuerySpec struct {
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The name of the VerticaDB CR that this VerticaRestorePointsQuery is defined for.  The
	// VerticaDB object must exist in the same namespace as this object.
	VerticaDBName string `json:"verticaDBName"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Optional parameter that will limit the query to only restore points satisfying provided filter options
	FilterOptions *VerticaRestorePointQueryFilterOptions `json:"filterOptions,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=ShowRestorePoints
	// +kubebuilder:validation:Enum=SaveRestorePoint;ShowRestorePoints
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:ShowRestorePoints","urn:alm:descriptor:com.tectonic.ui:select:SaveRestorePoint"}
	// The type of restore points query to perform
	QueryType VerticaRestorePointsQueryType `json:"queryType"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Options to use when saving a restore point; required if QueryType is SaveRestorePoint
	SaveOptions *SaveRestorePointOptions `json:"saveOptions,omitempty"`
}

type VerticaRestorePointsQueryType string

const (
	// RestorePointsQueryTypeList indicates a query to list restore points
	SaveRestorePoint VerticaRestorePointsQueryType = "SaveRestorePoint"
	// RestorePointsQueryTypeShow indicates a query to show restore points
	ShowRestorePoints VerticaRestorePointsQueryType = "ShowRestorePoints"
)

// VerticaRestorePointQueryFilterOptions defines the filter options to use while listing restore points
type VerticaRestorePointQueryFilterOptions struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional parameter that will limit the query to only restore points from this archive
	ArchiveName string `json:"archiveName,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional parameter that will limit the query to only restore points created at this timestamp or after this timestamp;
	// the timestamp can be of date time format or date only format, e.g. "2006-01-02", "2006-01-02 15:04:05", "2006-01-02 15:04:05.000000000";
	// the timestamp is interpreted as in UTC timezone
	StartTimestamp string `json:"startTimestamp,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional parameter that will limit the query to only restore points created at this timestamp or before timestamp;
	// the timestamp can be of date time format or date only format, e.g. "2006-01-02", "2006-01-02 15:04:05", "2006-01-02 15:04:05.000000000";
	// the timestamp is interpreted as in UTC timezone
	EndTimestamp string `json:"endTimestamp,omitempty"`
}

type SaveRestorePointOptions struct {
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	Archive string `json:"archive"`

	// +kubebuilder:default:=0
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Maximum number of restore points to save for this archive.
	NumRestorePoints int `json:"numRestorePoints,omitempty"`
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

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// This contains the result of the restore points query. Check the QueryComplete
	// status condition to know when this has been populated by the operator.
	RestorePoints []vclusterops.RestorePoint `json:"restorePoints"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Details of the saved restore point. This is only populated if the queryType
	// is SaveRestorePoint.
	SavedRestorePoint *RestorePointInfo `json:"savedRestorePoint,omitempty"`
}

type RestorePointInfo struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Name of the archive that this restore point was created in.
	Archive string `json:"archive"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	StartTimestamp string `json:"startTimestamp"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	EndTimestamp string `json:"endTimestamp"`
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
// +operator-sdk:csv:customresourcedefinitions:resources={{VerticaDB,vertica.com/v1,""}}

// VerticaRestorePointsQuery is the Schema for the verticarestorepointsqueries API
type VerticaRestorePointsQuery struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaRestorePointsQuerySpec   `json:"spec,omitempty"`
	Status VerticaRestorePointsQueryStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// VerticaRestorePointsQueryList contains a list of VerticaRestorePointsQuery
type VerticaRestorePointsQueryList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaRestorePointsQuery `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaRestorePointsQuery{}, &VerticaRestorePointsQueryList{})
}
