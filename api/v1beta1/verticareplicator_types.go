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
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Information of the source Vertica database to replicate from
	Source VerticaReplicatorDatabaseInfo `json:"source"`

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Information of the target Vertica database to replicate to
	Target VerticaReplicatorDatabaseInfo `json:"target"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional TLS configuration to use when connecting from the source database to the target database;
	// it refers to an existing TLS config that already exists in the source
	TLSConfig string `json:"tlsConfig,omitempty"`
}

// VerticaReplicatorDatabaseInfo defines the information related to either the source or target Vertica database
// involved in a replication
type VerticaReplicatorDatabaseInfo struct {
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Name of either source or target Vertica database
	VerticaDB string `json:"verticaDB"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional name of the sandbox in either source or target Vertica database;
	// if omitted or empty the main cluster of the database is assumed
	SandboxName string `json:"sandboxName,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional username with which to connect to either source or target Vertica database;
	// if omitted the superuser of the database is assumed
	UserName string `json:"userName,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// Optional name of the secret which contains password for the specified username with which to
	// connect to either source or target Vertica database;
	// if omitted or empty no password is provided;
	// the secret is assumed to be a k8s secret;
	// if it has the secret path reference (i.e. gsm://), it reads the secret
	// from the external secret storage manager
	PasswordSecret string `json:"passwordSecret,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional name of the service that will be connected to in either source or target Vertica database;
	// if omitted or empty the service object for the first primary subcluster is assumed
	ServiceName string `json:"serviceName,omitempty"`
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

const (
	// Replicating indicates whether the operator is currently conducting the database replication
	Replicating = "Replicating"
	// ReplicationComplete indicates the database replication has been completed
	ReplicationComplete = "ReplicationComplete"
	// ReplicationReady indicates whether the operator is ready to start the database replication
	ReplicationReady = "ReplicationReady"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=verticareplicators,singular=verticareplicator,categories=all;vertica,shortName=vrep
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
