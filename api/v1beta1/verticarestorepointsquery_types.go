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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
	// Optional parameter that will limit the query to only restore points
	// from this archvie
	ArchiveName string `json:"archiveName"`
}

const (
	ArchiveNm = "backup"
)

// VerticaRestorePointsQueryStatus defines the observed state of VerticaRestorePointsQuery
type VerticaRestorePointsQueryStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

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

func (vrqb *VerticaRestorePointsQuery) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      vrqb.ObjectMeta.Name,
		Namespace: vrqb.ObjectMeta.Namespace,
	}
}

func MakeSampleVrqbName() types.NamespacedName {
	return types.NamespacedName{Name: "vrqb-sample", Namespace: "default"}
}

// MakeVrqb will make an VerticaRestorePointsQuery for test purposes
func MakeVrqb() *VerticaRestorePointsQuery {
	VDBNm := MakeVDBName()
	nm := MakeSampleVrqbName()
	return &VerticaRestorePointsQuery{
		TypeMeta: metav1.TypeMeta{
			APIVersion: GroupVersion.String(),
			Kind:       RestorePointsQueryKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      nm.Name,
			Namespace: nm.Namespace,
			UID:       "zxcvbn-ghi-lkm",
		},
		Spec: VerticaRestorePointsQuerySpec{
			VerticaDBName: VDBNm.Name,
			ArchiveName:   ArchiveNm,
		},
	}
}