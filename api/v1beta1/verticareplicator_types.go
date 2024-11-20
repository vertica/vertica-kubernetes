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
	// Information of the source Vertica database to replicate from
	Source VerticaReplicatorSourceDatabaseInfo `json:"source"`

	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Information of the target Vertica database to replicate to
	Target VerticaReplicatorTargetDatabaseInfo `json:"target"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Optional TLS configuration to use when connecting from the source
	// database to the target database.
	// It refers to an existing TLS config that already exists in the source.
	// Using TLS configuration for target database authentication requires the
	// same username to be used for both source and target databases. It also
	// requires security config parameter EnableConnectCredentialForwarding to
	// be enabled on the source database. Custom username for source and target
	// databases is not supported yet when TLS configuration is used.
	TLSConfig string `json:"tlsConfig,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=async
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Determines how replication is done. Available options: async, sync
	Mode string `json:"mode"`
}

type VerticaReplicatorSourceDatabaseInfo struct {
	VerticaReplicatorDatabaseInfo `json:",inline"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The object name we want to copy from the source side. The available types are: namespace, schema, table.
	// If this is omitted, the operator will replicate all namespaces in the source database.
	ObjectName string `json:"objectName,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// A string containing a wildcard pattern of the schemas and/or tables to include in the replication.
	// Namespace names must be front-qualified with a period.
	IncludePattern string `json:"includePattern,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// A string containing a wildcard pattern of the schemas and/or tables to exclude from the set of tables matched
	// by the include pattern. Namespace names must be front-qualified with a period.
	ExcludePattern string `json:"excludePattern,omitempty"`
}

type VerticaReplicatorTargetDatabaseInfo struct {
	VerticaReplicatorDatabaseInfo `json:",inline"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Namespace in the target database to which objects are replicated. The target namespace must have the same shard
	// count as the source namespace in the source cluster. If you do not specify a target namespace, objects are
	// replicated to a namespace with the same name as the source namespace. If no such namespace exists in the target
	// cluster, it is created with the same name and shard count as the source namespace. You can only replicate tables
	// in the public schema to the default_namespace in the target cluster.
	Namespace string `json:"namespace,omitempty"`
}

// VerticaReplicatorDatabaseInfo defines the information related to either the source or target Vertica database
// involved in a replication
type VerticaReplicatorDatabaseInfo struct {
	// +kubebuilder:validation:Required
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Name of an existing VerticaDB
	VerticaDB string `json:"verticaDB"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// Specify the sandbox name to establish a connection. If no sandbox name is
	// provided, the system assumes the main cluster of the database.
	SandboxName string `json:"sandboxName,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// The username to connect to Vertica with. If no username is specified, the
	// database's superuser will be assumed. Custom username for source database
	// is not supported yet.
	UserName string `json:"userName,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// The password secret for the given UserName is stored in this field. If
	// this field and UserName are omitted, the default is set to the superuser
	// password secret found in the VerticaDB. An empty value indicates the
	// absence of a password.
	//
	// The secret is assumed to be a Kubernetes (k8s) secret unless a secret
	// path reference is specified. In the latter case, the secret is retrieved
	// from an external secret storage manager.
	PasswordSecret string `json:"passwordSecret,omitempty"`
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:text"
	// This field allows you to specify the name of the service object that will
	// be used to connect to the database. If you do not specify a name, the
	// service object for the first primary subcluster will be used.
	ServiceName string `json:"serviceName,omitempty"`
}

// VerticaReplicatorStatus defines the observed state of VerticaReplicator
type VerticaReplicatorStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Status message for replicator
	State string `json:"state,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Transaction ID for async replication
	TransactionID int64 `json:"transactionID,omitempty"`

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

	// ReplicationReady indicates database replication should be done asynchronously
	ReplicationModeAsync = "async"
	// ReplicationReady indicates database replication should be done synchronously
	ReplicationModeSync = "sync"
)

//+kubebuilder:object:root=true
//+kubebuilder:resource:path=verticareplicators,singular=verticareplicator,categories=all;vertica,shortName=vrep
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="SourceVerticaDB",type="string",JSONPath=".spec.source.verticaDB"
//+kubebuilder:printcolumn:name="TargetVerticaDB",type="string",JSONPath=".spec.target.verticaDB"
//+kubebuilder:printcolumn:name="State",type="string",JSONPath=".status.state"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
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

// IsUsingAsyncReplication returns true if replication mode is set to async
func (vrep *VerticaReplicator) IsUsingAsyncReplication() bool {
	return vrep.Spec.Mode == ReplicationModeAsync
}
