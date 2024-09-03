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
package v1

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// VerticaDBSpec defines the desired state of VerticaDB
type VerticaDBSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=IfNotPresent
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:imagePullPolicy"
	// This dictates the image pull policy to use
	ImagePullPolicy corev1.PullPolicy `json:"imagePullPolicy,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// ImagePullSecrets is an optional list of references to secrets in the same
	// namespace to use for pulling the image. If specified, these secrets will
	// be passed to individual puller implementations for them to use. For
	// example, in the case of docker, only DockerConfig type secrets are
	// honored.
	// More info: https://kubernetes.io/docs/concepts/containers/images#specifying-imagepullsecrets-on-a-pod
	ImagePullSecrets []LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="opentext/vertica-k8s:24.1.0-0-minimal"
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The docker image name that contains the Vertica server.  Whenever this
	// changes, the operator treats this as an upgrade.  The upgrade can be done
	// either in an online or offline fashion.  See the upgradePolicy to
	// understand how to control the behavior.
	Image string `json:"image,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Custom labels that will be added to all of the objects that the operator
	// will create.
	Labels map[string]string `json:"labels,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Custom annotations that will be added to all of the objects that the
	// operator will create.
	Annotations map[string]string `json:"annotations,omitempty"`

	// +kubebuilder:default:=true
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:booleanSwitch","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// State to indicate whether the operator will restart Vertica if the
	// process is not running. Under normal cicumstances this is set to true.
	// The purpose of this is to allow a maintenance window, such as a
	// manual upgrade, without the operator interfering.
	AutoRestartVertica bool `json:"autoRestartVertica"`

	// +kubebuilder:default:="vertdb"
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// The name of the database.  This cannot be updated once the CRD is created.
	DBName string `json:"dbName"`

	// +kubebuilder:default:=6
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// The number of shards to create in the database. This cannot be updated
	// once the CRD is created.  Refer to this page to determine an optimal size:
	// https://www.vertica.com/docs/latest/HTML/Content/Authoring/Eon/SizingEonCluster.htm
	// The default was chosen using this link and the default subcluster size of 3.
	ShardCount int `json:"shardCount"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// An optional name for a secret that contains the password for the
	// database's superuser. If this is not set, then we assume no such password
	// is set for the database. If this is set, it is up the user to create this
	// secret before deployment. The secret must have a key named password. To
	// store this secret outside of Kubernetes, you can use a secret path
	// reference prefix, such as gsm://. Everything after the prefix is the name
	// of the secret in the service you are storing.
	PasswordSecret string `json:"passwordSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// The name of a secret that contains the contents of license files. The
	// secret must be in the same namespace as the CRD. Each of the keys in the
	// secret will be mounted as files in /home/dbadmin/licensing/mnt. If this
	// is set prior to creating a database, it will include one of the licenses
	// from the secret -- if there are multiple licenses it will pick one by
	// selecting the first one alphabetically.  The user is responsible for
	// installing any additional licenses or if the license was added to the
	// secret after DB creation.
	LicenseSecret string `json:"licenseSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=Create
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Create","urn:alm:descriptor:com.tectonic.ui:select:Revive","urn:alm:descriptor:com.tectonic.ui:select:ScheduleOnly"}
	// The initialization policy specifies how the database should be
	// configured. Options include creating a new database, reviving an existing
	// one, or simply scheduling pods. Possible values are Create, Revive,
	// CreateSkipPackageInstall, or ScheduleOnly.
	InitPolicy CommunalInitPolicy `json:"initPolicy"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:fieldDependency:initPolicy:Revive","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// Specifies the restore point details to use with this instance of the VerticaDB.
	RestorePoint *RestorePointPolicy `json:"restorePoint,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:Auto","urn:alm:descriptor:com.tectonic.ui:select:ReadOnlyOnline","urn:alm:descriptor:com.tectonic.ui:select:Online","urn:alm:descriptor:com.tectonic.ui:select:Offline"}
	// +kubebuilder:default:=Auto
	// This setting defines how the upgrade process will be managed. The
	// available values are Offline, ReadOnlyOnline, Online, and Auto.
	//
	// Offline: This option involves taking down the entire cluster and then
	// bringing it back up with the new image.
	//
	// ReadOnlyOnline: With this option, the cluster remains operational for reads
	// during the upgrade process. However, the data will be in read-only mode
	// until the Vertica nodes from the primary subcluster re-form the cluster
	// with the new image.
	//
	// Online: Similar to Online, this option keeps the cluster operational
	// throughout the upgrade process but allows writes. The cluster is split
	// into two replicas, and traffic is redirected to the active replica to
	// facilitate writes.
	//
	// Auto: This option selects one of the above methods automatically based on
	// compatibility with the version of Vertica you are running.
	UpgradePolicy UpgradePolicyType `json:"upgradePolicy"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:fieldDependency:initPolicy:Revive","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// This specifies the order of nodes when doing a revive.  Each entry
	// contains an index to a subcluster, which is an index in Subclusters[],
	// and a pod count of the number of pods include from the subcluster.
	//
	// For example, suppose the database you want to revive has the following setup:
	// v_db_node0001: subcluster A
	// v_db_node0002: subcluster A
	// v_db_node0003: subcluster B
	// v_db_node0004: subcluster A
	// v_db_node0005: subcluster B
	// v_db_node0006: subcluster B
	//
	// And the Subcluster[] array is defined as {'A', 'B'}.  The revive order
	// would be:
	// - {subclusterIndex:0, podCount:2}  # 2 pods from subcluster A
	// - {subclusterIndex:1, podCount:1}  # 1 pod from subcluster B
	// - {subclusterIndex:0, podCount:1}  # 1 pod from subcluster A
	// - {subclusterIndex:1, podCount:2}  # 2 pods from subcluster B
	//
	// If InitPolicy is not Revive, this field can be ignored.
	ReviveOrder []SubclusterPodCount `json:"reviveOrder,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Contains details about the communal storage.
	Communal CommunalStorage `json:"communal"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default={requestSize:"500Gi", dataPath:"/data", depotPath:"/depot"}
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Contain details about the local storage
	Local LocalStorage `json:"local"`

	//+operator-sdk:csv:customresourcedefinitions:type=spec
	Subclusters []Subcluster `json:"subclusters"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:io.kubernetes:ConfigMap","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// A config map that contains the contents of the /etc/hadoop directory.
	// This gets mounted in the container and is used to configure connections
	// to an HDFS communal path
	HadoopConfig string `json:"hadoopConfig,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// When doing an online upgrade, we designate a subcluster to
	// accept traffic while the other subclusters restart.  The designated
	// subcluster is specified here.  The name of the subcluster can refer to an
	// existing one or an entirely new subcluster.  If the subcluster is new, it
	// will exist only for the duration of the upgrade.  If this struct is
	// left empty the operator will default to picking existing subclusters.
	TemporarySubclusterRouting *SubclusterSelection `json:"temporarySubclusterRouting,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Optional sidecar containers that run along side the vertica server.  The
	// operator adds the same volume mounts that are in the vertica server
	// container to each sidecar container.
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Custom volumes that are added to sidecars and the Vertica container.
	// For these volumes to be visible in either container, they must have a
	// corresonding volumeMounts entry.  For sidecars, this is included in
	// `spec.sidecars[*].volumeMounts`.  For the Vertica container, it is
	// included in `spec.volumeMounts`.
	//
	// This accepts any valid volume type.  A unique name must be given for each
	// volume and it cannot conflict with any of the internally generated volumes.
	Volumes []corev1.Volume `json:"volumes,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Additional volume mounts to include in the Vertica container.  These
	// reference volumes that are in the Volumes list.  The mount path must not
	// conflict with a mount path that the operator adds internally.
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Secrets that will be mounted in the vertica container.  The purpose of
	// this is to allow custom certs to be available.  The full path is:
	//   /certs/<secretName>/<key_i>
	// Where <secretName> is the name provided in the secret and <key_i> is one
	// of the keys in the secret.
	CertSecrets []LocalObjectReference `json:"certSecrets,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:io.kubernetes:Secret","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// A secret that contains files required for Kereberos setup.  The secret
	// must have the following keys:
	// - krb5.conf: The contents of the Kerberos config file
	// - krb5.keytab: The keytab file that stores credentials for each Vertica principal.
	// These files will be mounted in /etc.  We use the same keytab file on each
	// host, so it must contain all of the Vertica principals.
	KerberosSecret string `json:"kerberosSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:advanced"}
	// Controls if the spread communication between pods is encrypted.  Valid
	// values are 'vertica', 'disabled', or an empty string.  By default,
	// the value is set to 'vertica' unless the user explicitly set it to
	// 'disabled'.  When set to 'vertica' or an empty string, Vertica generates
	// the spread encryption key for the cluster when the database starts up.
	//  This can only be set during initial creation of the CR.  If set for
	// initPolicy other than Create, then it has no effect.
	EncryptSpreadComm string `json:"encryptSpreadComm,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// Allows you to set any additional securityContext for the Vertica server
	// container.  We merge the values with the securityContext generated by the
	// operator.  The operator adds its own capabilities to this.  If you want
	// those capabilities to be removed you must explicitly include them in the
	// drop list.
	SecurityContext *corev1.SecurityContext `json:"securityContext,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// This can be used to override any pod-level securityContext for the
	// Vertica pod. It will be merged with the default context. If omitted, then
	// the default context is used.
	PodSecurityContext *corev1.PodSecurityContext `json:"podSecurityContext,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// +kubebuilder:default:=""
	// +kubebuilder:validation:Optional
	// A secret that contains the TLS credentials to use for Vertica's node
	// management agent (NMA). If this is empty, the operator will create a
	// secret to use and add the name of the generate secret in this field.
	// When set, the secret must have the following keys defined: tls.key,
	// tls.crt and ca.crt.  To store this secret outside of Kubernetes, you can
	// use a secret path reference prefix, such as gsm://. Everything after the
	// prefix is the name of the secret in the service you are storing.
	NMATLSSecret string `json:"nmaTLSSecret,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// +kubebuilder:validation:Optional
	// Allows tuning of the Vertica pods readiness probe. Each of the values
	// here are applied to the default readiness probe we create. If this is
	// omitted, we use the default probe.
	ReadinessProbeOverride *corev1.Probe `json:"readinessProbeOverride,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// +kubebuilder:validation:Optional
	// Allows tuning of the Vertica pods liveness probe. Each of the values
	// here are applied to the default liveness probe we create. If this is
	// omitted, we use the default probe.
	LivenessProbeOverride *corev1.Probe `json:"livenessProbeOverride,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// +kubebuilder:validation:Optional
	// Allows tuning of the Vertica pods startup probe. Each of the values
	// here are applied to the default startup probe we create. If this is
	// omitted, we use the default probe.
	StartupProbeOverride *corev1.Probe `json:"startupProbeOverride,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:io.kubernetes:ServiceAccount","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// +kubebuilder:validation:Optional
	// The name of a serviceAccount to use to run the Vertica pods. If the
	// service account is not specified or does not exist, the operator will
	// create one, using the specified name if provided, along with a Role and
	// RoleBinding.
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// +kubebuilder:validation:Optional
	// Identifies any sandboxes that exist for the database
	Sandboxes []Sandbox `json:"sandboxes,omitempty"`
}

// LocalObjectReference is used instead of corev1.LocalObjectReference and behaves the same.
// This is useful for the Openshift web console. This structure is used in some
// VerticaDB spec fields to define a list of secrets but, with the k8s',
// we could not add the "Secret" x-descriptor. By using this instead,
// we can add it and it (the x-descriptor) will take effect
// wherever this structure is used.
type LocalObjectReference struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// Name of the referent.
	// More info: https://kubernetes.io/docs/concepts/overview/working-with-objects/names/#names
	// +optional
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
}

// SubclusterSelection is used to select between existing subcluster by name
// or provide a template for a new subcluster.  This is used to specify what
// subcluster gets client routing for subcluster we are restarting during online
// upgrade.
type SubclusterSelection struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// Names of existing subclusters to use for temporary routing of client
	// connections.  The operator will use the first subcluster that is online.
	Names []string `json:"names,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// A new subcluster will be created using this as a template.  This
	// subcluster will only exist for the life of the online upgrade.  It
	// will accept client traffic for a subcluster that are in the process of
	// being restarted.
	Template Subcluster `json:"template,omitempty"`
}

type CommunalInitPolicy string

const (
	// The database in the communal path will be initialized with a create_db.
	// There must not already be a database in the communal path.
	CommunalInitPolicyCreate = "Create"
	// The database in the communal path will be initialized in the VerticaDB
	// through a revive_db.  The communal path must have a preexisting database.
	// This option could also be used to initialize the database by restoring
	// from a database archive if restorePoint field is properly specified.
	CommunalInitPolicyRevive = "Revive"
	// Only schedule pods to run with the vertica container.  The bootstrap of
	// the database, either create_db or revive_db, is not handled.  Use this
	// policy when you have a vertica cluster running outside of Kubernetes and
	// you want to provision new nodes to run inside Kubernetes.  Most of the
	// automation is disabled when running in this mode.
	CommunalInitPolicyScheduleOnly = "ScheduleOnly"
	// Like CommunalInitPolicyCreate, except it will skip the install of the
	// packages. This will speed up the time it takes to create the db. This is
	// only supported in Vertica release 12.0.1 or higher.
	CommunalInitPolicyCreateSkipPackageInstall = "CreateSkipPackageInstall"
)

// RestorePointPolicy is used to locate the exact archive and restore point within archive
// when a database restore is intended
type RestorePointPolicy struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// Name of the restore archive to use for bootstrapping.
	// This name refers to an object in the database.
	// This must be specified if initPolicy is Revive and a restore is intended.
	Archive string `json:"archive,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:number"
	// +kubebuilder:validation:Optional
	// The (1-based) index of the restore point in the restore archive to restore from.
	// Specify either index or id exclusively; one of these fields is mandatory, but both cannot be used concurrently.
	Index int `json:"index,omitempty"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:validation:Optional
	// The identifier of the restore point in the restore archive to restore from.
	// Specify either index or id exclusively; one of these fields is mandatory, but both cannot be used concurrently.
	ID string `json:"id,omitempty"`
}

// Set constant Upgrade Requeue Time
const URTime = 30

// Valid values for EncryptSpreadComm
const (
	EncryptSpreadCommWithVertica = "vertica"
	EncryptSpreadCommDisabled    = "disabled"
)

type UpgradePolicyType string

const (
	// Upgrade is done fully offline.  This means the cluster is stopped,
	// then restarted with the new image.
	OfflineUpgrade UpgradePolicyType = "Offline"
	// Upgrade is done online.  The primary subclusters are taken down first,
	// leaving the secondary subclusters in read-only mode.  When the primary
	// subcluster comes back up, we restart/remove all of the secondary
	// subclusters to take them out of read-only mode.
	ReadOnlyOnlineUpgrade UpgradePolicyType = "ReadOnlyOnline"
	// Like read-only online upgrade, however it allows for writes. This is done by
	// splitting the vertica cluster into two replicas, then following a
	// replication strategy where we failover to one of the replicas while the
	// other is being upgraded.
	OnlineUpgrade UpgradePolicyType = "Online"
	// This automatically picks between offline and online upgrade.  Online
	// can only be used if (a) a license secret exists since we may need to scale
	// out, (b) we are already on a minimum Vertica engine version that supports
	// read-only subclusters and (c) has a k-safety of 1.
	AutoUpgrade UpgradePolicyType = "Auto"
)

// SuperUser is an automatically-created user in database creation
const SuperUser = "dbadmin"

type HTTPServerModeType string

const (
	HTTPServerModeEnabled  HTTPServerModeType = "Enabled"
	HTTPServerModeDisabled HTTPServerModeType = "Disabled"
	HTTPServerModeAuto     HTTPServerModeType = "Auto"
)

type ServerSideEncryptionType string

const (
	SseS3  ServerSideEncryptionType = "SSE-S3"
	SseKMS ServerSideEncryptionType = "SSE-KMS"
	SseC   ServerSideEncryptionType = "SSE-C"
)

type DepotVolumeType string

const (
	EmptyDir         DepotVolumeType = "EmptyDir"
	PersistentVolume DepotVolumeType = "PersistentVolume"
)

// Defines a number of pods for a specific subcluster
type SubclusterPodCount struct {
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The index of the subcluster.  This is an index into Subclusters[]
	SubclusterIndex int `json:"subclusterIndex"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount"
	// The number of pods paired with this subcluster.  If this is omitted then,
	// all remaining pods in the subcluster will be used.
	PodCount int `json:"podCount,omitempty"`
}

// Holds details about the communal storage
type CommunalStorage struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The path to the communal storage. We support S3, Google Cloud Storage,
	// and HDFS paths.  The protocol in the path (e.g. s3:// or webhdfs://)
	// dictates the type of storage.  The path, whether it be a S3 bucket or
	// HDFS path, must exist prior to creating the VerticaDB.  When initPolicy
	// is Create, this field is required and the path must be empty.  When
	// initPolicy is Revive, this field is required and must be non-empty.
	Path string `json:"path"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The URL to the communal endpoint. The endpoint must be prefaced with http:// or
	// https:// to know what protocol to connect with. If using S3 or Google
	// Cloud Storage as communal storage and initPolicy is Create or Revive,
	// this field is required and cannot change after creation.
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// The name of an optional secret that contains the credentials to connect to the
	// communal endpoint. This can be omitted if the communal storage uses some
	// other form of authentication such as an attached IAM profile in AWS.
	// Certain keys need to be set, depending on the endpoint type. If the
	// communal storage starts with s3:// or gs://, the secret must have the
	// following keys set: accesskey and secretkey. If the communal storage
	// starts with azb://, the secret can have the following keys: accountName,
	// blobEndpoint, accountKey, or sharedAccessSignature. To store this secret
	// outside of Kubernetes, you can use a secret path reference prefix, such
	// as gsm://. Everything after the prefix is the name of the secret in the
	// service you are storing.
	CredentialSecret string `json:"credentialSecret"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The absolute path to a certificate bundle of trusted CAs. This CA bundle
	// is used when establishing TLS connections to external services such as
	// AWS, Azure or swebhdf:// scheme.  Typically this would refer to a path to
	// one of the certSecrets.
	CaFile string `json:"caFile,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The region containing the bucket.  If you do not set the correct
	// region, you might experience a delay before the bootstrap fails because
	// Vertica retries several times before giving up.
	Region string `json:"region,omitempty"`

	// +kubebuilder:default:=""
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:SSE-S3","urn:alm:descriptor:com.tectonic.ui:select:SSE-KMS","urn:alm:descriptor:com.tectonic.ui:select:SSE-C"}
	// The server-side encryption type Vertica will use to read/write from encrypted S3 communal storage.
	// Available values are: SSE-S3, SSE-KMS, SSE-C and empty string ("").
	// - SSE-S3: the S3 service manages encryption keys.
	// - SSE-KMS: encryption keys are managed by the Key Management Service (KMS).
	// 	 KMS key identifier must be supplied through communal.additionalConfig map.
	// - SSE-C: the client manages encryption keys and provides them to S3 for each operation.
	// 	 The client key must be supplied through communal.s3SseCustomerKeySecret.
	// - Empty string (""): No encryption. This is the default value.
	// This value cannot change after the initial creation of the VerticaDB.
	S3ServerSideEncryption ServerSideEncryptionType `json:"s3ServerSideEncryption,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:Secret"
	// The name of a secret that contains the key to use for the S3SseCustomerKey config option in the server.
	// It is required when S3ServerSideEncryption is SSE-C. When set, the secret must have a key named clientKey.
	S3SseCustomerKeySecret string `json:"s3SseCustomerKeySecret,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Contains a map of server configuration parameters.
	// To avoid duplicate values, if a parameter is already set through another CR field,
	// (like S3ServerSideEncryption through communal.s3ServerSideEncryption), the corresponding
	// key/value pair is skipped. If a config value is set that isn't supported by the server version
	// you are running, the server will fail to start. These are set only during initial bootstrap. After
	// the database has been initialized, changing the options in the CR will have no affect in the server.
	AdditionalConfig map[string]string `json:"additionalConfig,omitempty"`
}

type LocalStorage struct {
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:io.kubernetes:StorageClass"
	// The local data stores the local catalog, depot and config files. Portions
	// of the local data are persisted with a persistent volume (PV) using a
	// persistent volume claim (PVC). The catalog and config files are always
	// stored in the PV. The depot may be include too if depotVolume is set to
	// 'PersistentVolume'. This field is used to define the name of the storage
	// class to use for the PV. This will be set when creating the PVC. By
	// default, it is not set. This means that that the PVC we create will have
	// the default storage class set in Kubernetes.
	StorageClass string `json:"storageClass,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="500Gi"
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The minimum size of the local data volume when picking a PV.  If changing
	// this after the PV have been created, two things may happen. First, it
	// will cause a resize of the PV to the new size. And, if depot is
	// stored in the PV, a resize of the depot happens too.
	RequestSize resource.Quantity `json:"requestSize,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=/data
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The path in the container to the local directory for the 'DATA,TEMP'
	// storage location usage. When initializing the database with revive, the
	// local path here must match the path that was used when the database was
	// first created.
	DataPath string `json:"dataPath"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=/depot
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The path in the container to the depot -- 'DEPOT' storage location usage.
	// When initializing the database with revive, this path must match the
	// depot path used when the database was first created.
	DepotPath string `json:"depotPath"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:PersistentVolume","urn:alm:descriptor:com.tectonic.ui:select:EmptyDir"}
	// The type of volume to use for the depot.
	// Allowable values will be: EmptyDir and PersistentVolume or an empty string.
	// An empty string currently defaults to PersistentVolume.
	DepotVolume DepotVolumeType `json:"depotVolume,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The path in the container to the catalog.  When initializing the database with
	// revive, this path must match the catalog path used when the database was
	// first created. For backwards compatibility, if this is omitted, then it
	// shares the same path as the dataPath.
	CatalogPath string `json:"catalogPath"`
}

// GetCatalogPath returns the path to the catalog. This wrapper exists because
// earlier versions of the API never had a catalog path. So we default to
// dataPath in that case.
func (l *LocalStorage) GetCatalogPath() string {
	if l.CatalogPath == "" {
		return l.DataPath
	}
	return l.CatalogPath
}

// IsDepotPathUnique returns true is depot path is different from
// catalog and data paths.
func (l *LocalStorage) IsDepotPathUnique() bool {
	return l.DepotPath != l.DataPath &&
		l.DepotPath != l.GetCatalogPath()
}

type Sandbox struct {
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// This is the name of a sandbox. This is a required parameter. This cannot
	// change once the sandbox is created.
	Name string `json:"name"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// +kubebuilder:default:=""
	// +kubebuilder:validation:Optional
	// The name of the image to use for the sandbox. If omitted, the image
	// is inherited from the spec.image field.
	Image string `json:"image,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// This is the subcluster names that are part of the sandbox.
	// There must be at least one subcluster listed. All subclusters
	// listed need to be secondary subclusters.
	Subclusters []SubclusterName `json:"subclusters"`
}

type SubclusterName struct {
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The name of a subcluster.
	Name string `json:"name"`
}

type Subcluster struct {
	// +kubebuilder:validation:required
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The name of the subcluster. This is a required parameter. This cannot
	// change after CRD creation.
	Name string `json:"name"`

	// +kubebuilder:default:=3
	// +kubebuilder:Minimum:=3
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podCount"
	// The number of pods that the subcluster will have. This determines the
	// number of Vertica nodes that it will have. Changing this number will
	// either delete or schedule new pods.
	//
	// The database has a k-safety of 1. So, if this is a primary subcluster,
	// the minimum value is 3. If this is a secondary subcluster, the minimum is
	// 0.
	//
	// Note, you must have a valid license to pick a value larger than 3. The
	// default license that comes in the vertica container is for the community
	// edition, which can only have 3 nodes. The license can be set with the
	// db.licenseSecret parameter.
	Size int32 `json:"size"`

	// +kubebuilder:default:=primary
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:primary","urn:alm:descriptor:com.tectonic.ui:select:secondary"}
	// Indicates the type of subcluster it is. Valid values are: primary,
	// secondary or transient. Types are case-sensitive.
	// You must have at least one primary subcluster in the database.
	// If type is omitted, it will default to a primary.
	// Transient should only be set internally by the operator during online
	// upgrade. It is used to indicate a subcluster that exists temporarily to
	// serve traffic for subclusters that are restarting with a new image.
	Type string `json:"type"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:hidden"
	// This allows a different image to be used for the subcluster than the one
	// in VerticaDB.  This is intended to be used internally by the online image
	// change process.
	ImageOverride string `json:"imageOverride,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A map of label keys and values to restrict Vertica node scheduling to workers
	// with matching labels.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Like nodeSelector this allows you to constrain the pod only to certain
	// pods. It is more expressive than just using node selectors.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity
	Affinity Affinity `json:"affinity,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// The priority class name given to pods in this subcluster. This affects
	// where the pod gets scheduled.
	// More info: https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Any tolerations and taints to use to aid in where to schedule a pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:resourceRequirements"
	// This defines the resource requests and limits for pods in the subcluster.
	// It is advisable that the request and limits match as this ensures the
	// pods are assigned to the guaranteed QoS class. This will reduces the
	// chance that pods are chosen by the OOM killer.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +kubebuilder:default:=ClusterIP
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:select:ClusterIP","urn:alm:descriptor:com.tectonic.ui:select:NodePort","urn:alm:descriptor:com.tectonic.ui:select:LoadBalancer"}
	// Identifies the type of Kubernetes service to use for external client
	// connectivity. The default is to use a ClusterIP, which sets a stable IP
	// and port to use that is accessible only from within Kubernetes itself.
	// Depending on the service type chosen the user may need to set other
	// config knobs to further config it. These other knobs follow this one.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Identifies the name of the service object that will serve this
	// subcluster.  If multiple subclusters share the same service name then
	// they all share the same service object.  This allows for a single service
	// object to round robin between multiple subclusters.  If this is left
	// blank, a service object matching the subcluster name is used.  The actual
	// name of the service object is always prefixed with the name of the owning
	// VerticaDB.
	ServiceName string `json:"serviceName,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// When setting serviceType to NodePort, this parameter allows you to define the
	// port that is opened at each node for Vertica client connections. If using
	// NodePort and this is omitted, Kubernetes will choose the port
	// automatically. This port must be from within the defined range allocated
	// by the control plane (default is 30000-32767).
	ClientNodePort int32 `json:"clientNodePort,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors={"urn:alm:descriptor:com.tectonic.ui:number","urn:alm:descriptor:com.tectonic.ui:advanced"}
	// Like the clientNodePort parameter, except this controls the node port to use
	// for the http endpoint in the Vertica server.  The same rules apply: it
	// must be defined within the range allocated by the control plane, if
	// omitted Kubernetes will choose the port automatically.
	VerticaHTTPNodePort int32 `json:"verticaHTTPNodePort,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Allows the service object to be attached to a list of external IPs that you
	// specify. If not set, the external IP list is left empty in the service object.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#external-ips
	ExternalIPs []string `json:"externalIPs,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:advanced"
	// Specify IP address of LoadBalancer service for this subcluster.
	// This field is ignored when serviceType != "LoadBalancer".
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#loadbalancer
	LoadBalancerIP string `json:"loadBalancerIP,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A map of key/value pairs appended to service metadata.annotations.
	ServiceAnnotations map[string]string `json:"serviceAnnotations,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A map of key/value pairs appended to the stateful metadata.annotations of
	// the subcluster.
	Annotations map[string]string `json:"annotations,omitempty"`
}

// Affinity is used instead of corev1.Affinity and behaves the same.
// This structure is used in subcluster to define the "Affinity".
// corev1.Affinity is composed of 3 fields and for each of them,
// there is a x-descriptor. However there is not a x-descriptor for corev1.Affinity itself.
// In this structure, we have the same fields as corev1' but we also added
// the corresponding x-descriptor to each field. That will be useful for the Openshift web console.
type Affinity struct {
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:nodeAffinity"
	// Describes node affinity scheduling rules for the pod.
	// +optional
	NodeAffinity *corev1.NodeAffinity `json:"nodeAffinity,omitempty" protobuf:"bytes,1,opt,name=nodeAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAffinity"
	// Describes pod affinity scheduling rules (e.g. co-locate this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAffinity *corev1.PodAffinity `json:"podAffinity,omitempty" protobuf:"bytes,2,opt,name=podAffinity"`
	// +operator-sdk:csv:customresourcedefinitions:type=spec,xDescriptors="urn:alm:descriptor:com.tectonic.ui:podAntiAffinity"
	// Describes pod anti-affinity scheduling rules (e.g. avoid putting this pod in the same node, zone, etc. as some other pod(s)).
	// +optional
	PodAntiAffinity *corev1.PodAntiAffinity `json:"podAntiAffinity,omitempty" protobuf:"bytes,3,opt,name=podAntiAffinity"`
}

// VerticaDBStatus defines the observed state of VerticaDB
type VerticaDBStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// A count of the number of pods that have been added to the database.
	AddedToDBCount int32 `json:"addedToDBCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// A count of the number of pods that have a running vertica process.
	UpNodeCount int32 `json:"upNodeCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The number of subclusters in the database
	SubclusterCount int32 `json:"subclusterCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Status per subcluster.
	Subclusters []SubclusterStatus `json:"subclusters,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Conditions for VerticaDB
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Status message for the current running upgrade.   If no upgrade
	// is occurring, this message remains blank.
	UpgradeStatus string `json:"upgradeStatus"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// The sandbox statuses
	Sandboxes []SandboxStatus `json:"sandboxes,omitempty"`
}

type SandboxUpgradeState struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// UpgradeInProgress indicates if the sandbox is in the process
	// of having its image change
	UpgradeInProgress bool `json:"upgradeInProgress"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// Status message for the current running upgrade. If no upgrade
	// is occurring, this message remains blank.
	UpgradeStatus string `json:"upgradeStatus"`
}

type SandboxStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Name of the sandbox that was defined in the spec
	Name string `json:"name"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// The names of subclusters that are currently a part of the given sandbox.
	// This is updated as subclusters become sandboxed or unsandboxed.
	Subclusters []string `json:"subclusters"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// +optional
	// State of the current running upgrade in the sandbox
	UpgradeState SandboxUpgradeState `json:"upgradeState,omitempty"`
}

const (
	// AutoRestartVertica indicates whether the operator should restart the vertica process
	AutoRestartVertica = "AutoRestartVertica"
	// DBInitialized indicates the database has been created or revived
	DBInitialized = "DBInitialized"
	// UpgradeInProgress indicates if the vertica server is in the process
	// of having its image change.  We have additional conditions to
	// distinguish between the different types of upgrade it is.
	UpgradeInProgress               = "UpgradeInProgress"
	OfflineUpgradeInProgress        = "OfflineUpgradeInProgress"
	ReadOnlyOnlineUpgradeInProgress = "ReadOnlyOnlineUpgradeInProgress"
	OnlineUpgradeInProgress         = "OnlineUpgradeInProgress"
	// VerticaRestartNeeded is a condition that when set to true will force the
	// operator to stop/start the vertica pods.
	VerticaRestartNeeded    = "VerticaRestartNeeded"
	SaveRestorePointsNeeded = "SaveRestorePointsNeeded"
)

const (
	// list of reasons for conditions' transitions
	UnknownReason = "UnKnown"
)

const (
	// The different type names for the subcluster type. A webhook exists to
	// allow the types to be set as any type.
	PrimarySubcluster   = "primary"
	SecondarySubcluster = "secondary"
	TransientSubcluster = "transient"
	// A sandbox primary subcluster is a secondary subcluster that was the first
	// subcluster in a sandbox. These subclusters are primaries when they are
	// sandboxed. When unsandboxed, they will go back to being just a secondary
	// subcluster
	SandboxPrimarySubcluster = "sandboxprimary"
)

// SubclusterStatus defines the per-subcluster status that we track
type SubclusterStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Name of the subcluster
	Name string `json:"name"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// Object ID of the subcluster.
	Oid string `json:"oid"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// A count of the number of pods that have been added to the database for this subcluster.
	AddedToDBCount int32 `json:"addedToDBCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	// A count of the number of pods that have a running vertica process in this subcluster.
	UpNodeCount int32 `json:"upNodeCount"`

	// +operator-sdk:csv:customresourcedefinitions:type=status
	Detail []VerticaDBPodStatus `json:"detail"`
}

// VerticaDBPodStatus holds state for a single pod in a subcluster
type VerticaDBPodStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// This is set to true if /opt/vertica/config has been bootstrapped.
	Installed bool `json:"installed"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// This is set to true if the DB exists and the pod has been added to it.
	AddedToDB bool `json:"addedToDB"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// This is the vnode name that Vertica internally assigned this pod (e.g. v_<dbname>_nodexxxx)
	VNodeName string `json:"vnodeName"`
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// True means the vertica process is running on this pod and it can accept
	// connections on port 5433.
	UpNode bool `json:"upNode"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:resource:categories=all;vertica,shortName=vdb
//+kubebuilder:printcolumn:name="Subclusters",type="integer",JSONPath=".status.subclusterCount"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +operator-sdk:csv:customresourcedefinitions:resources={{Statefulset,apps/v1,""},{Pod,v1,""},{Service,v1,""}}

// VerticaDB is the CR that defines a Vertica Eon mode cluster that is managed by the verticadb-operator.
type VerticaDB struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   VerticaDBSpec   `json:"spec,omitempty"`
	Status VerticaDBStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// VerticaDBList contains a list of VerticaDB
type VerticaDBList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []VerticaDB `json:"items"`
}

func init() {
	SchemeBuilder.Register(&VerticaDB{}, &VerticaDBList{})
}
