/*
Copyright 2021.

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
	"fmt"
	"regexp"

	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Important: Run "make" to regenerate code after modifying this file

const VerticaDBKind = "VerticaDB"
const VerticaDBAPIVersion = "vertica.com/v1beta1"

// VerticaDBSpec defines the desired state of VerticaDB
type VerticaDBSpec struct {
	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=IfNotPresent
	// +operator-sdk:csv:customresourcedefinitions:type=spec
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
	ImagePullSecrets []corev1.LocalObjectReference `json:"imagePullSecrets,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="vertica/vertica-k8s:11.0.1-0-minimal"
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The docker image name that contains Vertica.  Whenever this changes, the
	// operator treats this as an upgrade and will stop the entire cluster and
	// restart it with the new image.
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
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// State to indicate whether the operator will restart Vertica if the
	// process is not running. Under normal cicumstances this is set to true.
	// The purpose of this is to allow a maintenance window, such as a
	// manual upgrade, without the operator interfering.
	AutoRestartVertica bool `json:"autoRestartVertica"`

	// +kubebuilder:default:="vertdb"
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The name of the database.  This cannot be updated once the CRD is created.
	DBName string `json:"dbName"`

	// +kubebuilder:default:=12
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The number of shards to create in the database. This cannot be updated
	// once the CRD is created.
	ShardCount int `json:"shardCount"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// An optional name for a secret that contains the password for the
	// database's superuser. If this is not set, then we assume no such password
	// is set for the database. If this is set, it is up the user to create this
	// secret before deployment. The secret must have a key named password.
	SuperuserPasswordSecret string `json:"superuserPasswordSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
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
	// +kubebuilder:default=false
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Ignore the cluster lease when doing a revive or start_db.  Use this with
	// caution, as ignoring the cluster lease when another system is using the
	// same communal storage will cause corruption.
	IgnoreClusterLease bool `json:"ignoreClusterLease,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=Create
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// The initialization policy defines how to setup the database.  Available
	// options are to create a new database or revive an existing one.
	InitPolicy CommunalInitPolicy `json:"initPolicy"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
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
	// The timeout, in seconds, to use when admintools restarts a node or the
	// entire cluster.  If omitted, we use the admintools default timeout
	// of 20 minutes.
	RestartTimeout int `json:"restartTimeout,omitempty"`

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

	// +kubebuilder:default:="1"
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Sets the fault tolerance for the cluster.  Allowable values are 0 or 1.  0 is only
	// suitable for test environments because we have no fault tolerance and the cluster
	// can only have between 1 and 3 pods.  If set to 1, we have fault tolerance if nodes
	// die and the cluster has a minimum of 3 pods.
	//
	// This value cannot change after the initial creation of the VerticaDB.
	KSafety KSafetyType `json:"kSafety,omitempty"`

	// +kubebuilder:default:=0
	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// If a reconciliation iteration needs to be requeued this controls the
	// amount of time in seconds to wait.  If this is set to 0, then the requeue
	// time will increase using an exponential backoff algorithm.  Caution, when
	// setting this to some positive value the exponential backoff is disabled.
	// This should be reserved for test environments as an error scenario could
	// easily consume the logs.
	RequeueTime int `json:"requeueTime,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Optional sidecar containers that run along side the vertica server.  The
	// operator adds the same volume mounts that are in the vertica server
	// container to each sidecar container.
	Sidecars []corev1.Container `json:"sidecars,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
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
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Additional volume mounts to include in the Vertica container.  These
	// reference volumes that are in the Volumes list.  The mount path must not
	// conflict with a mount path that the operator adds internally.
	VolumeMounts []corev1.VolumeMount `json:"volumeMounts,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// Secrets that will be mounted in the vertica container.  The purpose of
	// this is to allow custom certs to be available.  The full path is:
	//   /certs/<secretName>/<key_i>
	// Where <secretName> is the name provided in the secret and <key_i> is one
	// of the keys in the secret.
	CertSecrets []corev1.LocalObjectReference `json:"certSecrets,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// A secret that contains files required for Kereberos setup.  The secret
	// must have the following keys:
	// - krb5.conf: The contents of the Kerberos config file
	// - krb5.keytab: The keytab file that stores credentials for each Vertica principal.
	// These files will be mounted in /etc.  We use the same keytab file on each
	// host, so it must contain all of the Vertica principals.
	KerberosSecret string `json:"kerberosSecret,omitempty"`

	// +kubebuilder:validation:Optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec
	// An optional secret that has the files for /home/dbadmin/.ssh.  If this is
	// omitted, the ssh files from the image are used.  You can this option if
	// you have a cluster that talks to Vertica notes outside of Kubernetes, as
	// it has the public keys to be able to ssh to those nodes.  It must have
	// the following keys present: id_rsa, id_rsa.pub and authorized_keys.
	SSHSecret string `json:"sshSecret,omitempty"`
}

type CommunalInitPolicy string

const (
	// The database in the communal path will be initialized with a create_db.
	// There must not already be a database in the communal path.
	CommunalInitPolicyCreate = "Create"
	// The database in the communal path will be initialized in the VerticaDB
	// through a revive_db.  The communal path must have a preexisting database.
	CommunalInitPolicyRevive = "Revive"
	// Only schedule pods to run with the vertica container.  The bootstrap of
	// the database, either create_db or revive_db, is not handled.  Use this
	// policy when you have a vertica cluster running outside of Kubernetes and
	// you want to provision new nodes to run inside Kubernetes.  Most of the
	// automation is disabled when running in this mode.
	CommunalInitPolicyScheduleOnly = "ScheduleOnly"
)

type KSafetyType string

const (
	KSafety0 KSafetyType = "0"
	KSafety1 KSafetyType = "1"
)

// Defines a number of pods for a specific subcluster
type SubclusterPodCount struct {
	// +kubebuilder:validation:required
	// The index of the subcluster.  This is an index into Subclusters[]
	SubclusterIndex int `json:"subclusterIndex"`

	// +kubebuilder:validation:Optional
	// The number of pods paired with this subcluster.  If this is omitted then,
	// all remaining pods in the subcluster will be used.
	PodCount int `json:"podCount,omitempty"`
}

// Holds details about the communal storage
type CommunalStorage struct {
	// +kubebuilder:validation:Optional
	// The path to the communal storage. We support S3, Google Cloud Storage,
	// and HDFS paths.  The protocol in the path (e.g. s3:// or webhdfs://)
	// dictates the type of storage.  The path, whether it be a S3 bucket or
	// HDFS path, must exist prior to creating the VerticaDB.  When initPolicy
	// is Create, this field is required and the path must be empty.  When
	// initPolicy is Revive, this field is required and must be non-empty.
	Path string `json:"path"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=false
	// If true, the operator will include the VerticaDB's UID in the path.  This
	// option exists if you reuse the communal path in the same endpoint as it
	// forces each database path to be unique.
	IncludeUIDInPath bool `json:"includeUIDInPath,omitempty"`

	// +kubebuilder:validation:Optional
	// The URL to the communal endpoint. The endpoint must be prefaced with http:// or
	// https:// to know what protocol to connect with. If using S3 or Google
	// Cloud Storage as communal storage and initPolicy is Create or Revive,
	// this field is required and cannot change after creation.
	Endpoint string `json:"endpoint"`

	// +kubebuilder:validation:Optional
	// The name of a secret that contains the credentials to connect to the
	// communal endpoint (only applies to s3://, gs:// or azb://). Certain keys
	// need to be set, depending on the endpoint type:
	// - s3:// or gs:// - It must have the following keys set: accessey and secretkey.
	//     When using Google Cloud Storage, the IDs set in the secret are taken
	//     from the hash-based message authentication code (HMAC) keys.
	// - azb:// - It must have the following keys set:
	//     accountName - Name of the Azure account
	//     blobEndpoint - (Optional) Set this to the location of the endpoint.
	//       If using an emulator like Azurite, it can be set to something like
	//       'http://<IP-addr>:<port>'
	//     accountKey - If accessing with an account key set it here
	//     sharedAccessSignature - If accessing with a shared access signature,
	//     	  set it here
	//
	// When initPolicy is Create or Revive, and not using HDFS this field is
	// required.
	CredentialSecret string `json:"credentialSecret"`

	// +kubebuilder:validation:Optional
	// The absolute path to a certificate bundle of trusted CAs. This CA bundle
	// is used when establishing TLS connections to external services such as
	// AWS, Azure or swebhdf:// scheme.  Typically this would refer to a path to
	// one of the certSecrets.
	CaFile string `json:"caFile,omitempty"`

	// +kubebuilder:validation:Optional
	// The region containing the bucket.  If you do not set the correct
	// region, you might experience a delay before the bootstrap fails because
	// Vertica retries several times before giving up.
	Region string `json:"region,omitempty"`

	// +kubebuilder:validation:Optional
	// A config map that contains the contents of the /etc/hadoop directory.
	// This gets mounted in the container and is used to configure connections
	// to an HDFS communal path
	HadoopConfig string `json:"hadoopConfig,omitempty"`

	// +kubebuilder:validation:Optional
	// The service name portion of the Vertica Kerberos principal. This is set
	// in the database config parameter KerberosServiceName during bootstrapping.
	KerberosServiceName string `json:"kerberosServiceName,omitempty"`

	// +kubebuilder:validation:Optional
	// Name of the Kerberos realm.  This is set in the database config parameter
	// KerberosRealm during bootstrapping.
	KerberosRealm string `json:"kerberosRealm,omitempty"`
}

type LocalStorage struct {
	// +kubebuilder:validation:Optional
	// The local data stores the local catalog, depot and config files. This
	// defines the name of the storageClass to use for that volume. This will be
	// set when creating the PVC. By default, it is not set. This means that
	// that the PVC we create will have the default storage class set in
	// Kubernetes.
	StorageClass string `json:"storageClass,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:="500Gi"
	// The minimum size of the local data volume when picking a PV.
	RequestSize resource.Quantity `json:"requestSize,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=/data
	// The path in the container to the local catalog.  When initializing the
	// database with revive, the local path here must match the path that was
	// used when the database was first created.
	DataPath string `json:"dataPath"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default:=/depot
	// The path in the container to the depot.  When initializing the database with
	// revive, this path must match the depot path used when the database was
	// first created.
	DepotPath string `json:"depotPath"`
}

type Subcluster struct {
	// +kubebuilder:validation:required
	// The name of the subcluster. This is a required parameter. This cannot
	// change after CRD creation.
	Name string `json:"name"`

	// +kubebuilder:default:=3
	// +kubebuilder:Minimum:=3
	// +kubebuilder:validation:Optional
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

	// +kubebuilder:default:=true
	// +kubebuilder:validation:Optional
	// Indicates whether the subcluster is a primary or secondary. You must have
	// at least one primary subcluster in the database.
	IsPrimary bool `json:"isPrimary"`

	// A map of label keys and values to restrict Vertica node scheduling to workers
	// with matchiing labels.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#nodeselector
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`

	// Like nodeSelector this allows you to constrain the pod only to certain
	// pods. It is more expressive than just using node selectors.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/assign-pod-node/#affinity-and-anti-affinity
	Affinity *corev1.Affinity `json:"affinity,omitempty"`

	// The priority class name given to pods in this subcluster. This affects
	// where the pod gets scheduled.
	// More info: https://kubernetes.io/docs/concepts/configuration/pod-priority-preemption/#priorityclass
	PriorityClassName string `json:"priorityClassName,omitempty"`

	// Any tolerations and taints to use to aid in where to schedule a pod.
	// More info: https://kubernetes.io/docs/concepts/scheduling-eviction/taint-and-toleration/
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`

	// This defines the resource requests and limits for pods in the subcluster.
	// It is advisable that the request and limits match as this ensures the
	// pods are assigned to the guaranteed QoS class. This will reduces the
	// chance that pods are chosen by the OOM killer.
	// More info: https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/
	Resources corev1.ResourceRequirements `json:"resources,omitempty"`

	// +kubebuilder:default:=ClusterIP
	// +kubebuilder:validation:Optional
	// Identifies the type of Kubernetes service to use for external client
	// connectivity. The default is to use a ClusterIP, which sets a stable IP
	// and port to use that is accessible only from within Kubernetes itself.
	// Depending on the service type chosen the user may need to set other
	// config knobs to further config it. These other knobs follow this one.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types
	ServiceType corev1.ServiceType `json:"serviceType,omitempty"`

	// +kubebuilder:validation:Optional
	// When setting serviceType to NodePort, this parameter allows you to define the
	// port that is opened at each node. If using NodePort and this is omitted,
	// Kubernetes will choose the port automatically. This port must be from
	// within the defined range allocated by the control plane (default is
	// 30000-32767).
	NodePort int32 `json:"nodePort,omitempty"`

	// +kubebuilder:validation:Optional
	// Allows the service object to be attached to a list of external IPs that you
	// specify. If not set, the external IP list is left empty in the service object.
	// More info: https://kubernetes.io/docs/concepts/services-networking/service/#external-ips
	ExternalIPs []string `json:"externalIPs,omitempty"`
}

// VerticaDBStatus defines the observed state of VerticaDB
type VerticaDBStatus struct {
	// +operator-sdk:csv:customresourcedefinitions:type=status
	// A count of the number of pods that have been installed into the vertica cluster.
	InstallCount int32 `json:"installCount"`

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
	Conditions []VerticaDBCondition `json:"conditions,omitempty"`
}

// VerticaDBConditionType defines type for VerticaDBCondition
type VerticaDBConditionType string

const (
	// AutoRestartVertica indicates whether the operator should restart the vertica process
	AutoRestartVertica VerticaDBConditionType = "AutoRestartVertica"
	// DBInitialized indicates the database has been created or revived
	DBInitialized VerticaDBConditionType = "DBInitialized"
	// ImageChangeInProgress indicates if the vertica server is in the process of having its image change
	ImageChangeInProgress VerticaDBConditionType = "ImageChangeInProgress"
)

// Fixed index entries for each condition.
const (
	AutoRestartVerticaIndex = iota
	DBInitializedIndex
	ImageChangeInProgressIndex
)

// VerticaDBConditionIndexMap is a map of the VerticaDBConditionType to its
// index in the condition array
var VerticaDBConditionIndexMap = map[VerticaDBConditionType]int{
	AutoRestartVertica:    AutoRestartVerticaIndex,
	DBInitialized:         DBInitializedIndex,
	ImageChangeInProgress: ImageChangeInProgressIndex,
}

// VerticaDBConditionNameMap is the reverse of VerticaDBConditionIndexMap.  It
// maps an index to the condition name.
var VerticaDBConditionNameMap = map[int]VerticaDBConditionType{
	AutoRestartVerticaIndex:    AutoRestartVertica,
	DBInitializedIndex:         DBInitialized,
	ImageChangeInProgressIndex: ImageChangeInProgress,
}

// VerticaDBCondition defines condition for VerticaDB
type VerticaDBCondition struct {
	// Type is the type of the condition
	Type VerticaDBConditionType `json:"type"`

	// Status is the status of the condition
	// can be True, False or Unknown
	Status corev1.ConditionStatus `json:"status"`

	// Last time the condition transitioned from one status to another.
	// +optional
	LastTransitionTime metav1.Time `json:"lastTransitionTime,omitempty"`
}

// SubclusterStatus defines the per-subcluster status that we track
type SubclusterStatus struct {
	// Name of the subcluster
	Name string `json:"name"`

	// A count of the number of pods that have been installed into the subcluster.
	InstallCount int32 `json:"installCount"`

	// A count of the number of pods that have been added to the database for this subcluster.
	AddedToDBCount int32 `json:"addedToDBCount"`

	// A count of the number of pods that have a running vertica process in this subcluster.
	UpNodeCount int32 `json:"upNodeCount"`

	Detail []VerticaDBPodStatus `json:"detail"`
}

// VerticaDBPodStatus holds state for a single pod in a subcluster
type VerticaDBPodStatus struct {
	// This is set to true if /opt/vertica/config has been bootstrapped.
	Installed bool `json:"installed"`
	// This is set to true if the DB exists and the pod has been added to it.
	AddedToDB bool `json:"addedToDB"`
	// This is the vnode name that Vertica internally assigned this pod (e.g. v_<dbname>_nodexxxx)
	VNodeName string `json:"vnodeName"`
	// True means the vertica process is running on this pod and it can accept
	// connections on port 5433.
	UpNode bool `json:"upNode"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:categories=all;verticadbs,shortName=vdb
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
//+kubebuilder:printcolumn:name="Subclusters",type="integer",JSONPath=".status.subclusterCount"
//+kubebuilder:printcolumn:name="Installed",type="integer",JSONPath=".status.installCount"
//+kubebuilder:printcolumn:name="DBAdded",type="integer",JSONPath=".status.addedToDBCount"
//+kubebuilder:printcolumn:name="Up",type="integer",JSONPath=".status.upNodeCount"
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

const (
	// Annotations that we add by parsing vertica --version output
	VersionAnnotation   = "vertica.com/version"
	BuildDateAnnotation = "vertica.com/buildDate"
	BuildRefAnnotation  = "vertica.com/buildRef"

	DefaultS3Region       = "us-east-1"
	DefaultGCloudRegion   = "US-EAST1"
	DefaultGCloudEndpoint = "https://storage.googleapis.com"
)

// ExtractNamespacedName gets the name and returns it as a NamespacedName
func (v *VerticaDB) ExtractNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      v.ObjectMeta.Name,
		Namespace: v.ObjectMeta.Namespace,
	}
}

// MakeVDBName is a helper that creates a sample name for test purposes
func MakeVDBName() types.NamespacedName {
	return types.NamespacedName{Name: "vertica-sample", Namespace: "default"}
}

// MakeVDB is a helper that constructs a fully formed VerticaDB struct using the sample name.
// This is intended for test purposes.
func MakeVDB() *VerticaDB {
	nm := MakeVDBName()
	return &VerticaDB{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "vertica.com/v1beta1",
			Kind:       "VerticaDB",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        nm.Name,
			Namespace:   nm.Namespace,
			UID:         "abcdef-ghi",
			Annotations: make(map[string]string),
		},
		Spec: VerticaDBSpec{
			AutoRestartVertica: true,
			Labels:             make(map[string]string),
			Annotations:        make(map[string]string),
			Image:              "vertica-k8s:latest",
			InitPolicy:         CommunalInitPolicyCreate,
			Communal: CommunalStorage{
				Path:             "s3://nimbusdb/mspilchen",
				Endpoint:         "http://minio",
				CredentialSecret: "s3-auth",
			},
			Local: LocalStorage{
				DataPath:  "/data",
				DepotPath: "/depot",
			},
			DBName:     "db",
			ShardCount: 12,
			Subclusters: []Subcluster{
				{Name: "defaultsubcluster", Size: 3, ServiceType: corev1.ServiceTypeClusterIP},
			},
		},
	}
}

// GenSubclusterMap will organize all of the subclusters into a map for quicker lookup
func (v *VerticaDB) GenSubclusterMap() map[string]*Subcluster {
	scMap := map[string]*Subcluster{}
	for i := range v.Spec.Subclusters {
		sc := &v.Spec.Subclusters[i]
		scMap[sc.Name] = sc
	}
	return scMap
}

// IsValidSubclusterName validates the subcluster name is valid.  We have rules
// about its name because it is included in the name of the statefulset, so we
// must adhere to the Kubernetes rules for object names.
func IsValidSubclusterName(scName string) bool {
	r := regexp.MustCompile(`^[a-z0-9](?:[a-z0-9\-]{0,61}[a-z0-9])?$`)
	return r.MatchString(scName)
}

func (v *VerticaDB) GetVerticaVersion() (string, bool) {
	ver, ok := v.ObjectMeta.Annotations[VersionAnnotation]
	return ver, ok
}

// GenInstallerIndicatorFileName returns the name of the installer indicator file.
// Valid only for the current instance of the vdb.
func (v *VerticaDB) GenInstallerIndicatorFileName() string {
	return paths.InstallerIndicatorFile + string(v.UID)
}

// GetPVSubPath returns the subpath in the local data PV.
// We use the UID so that we create unique paths in the PV.  If the PV is reused
// for a new vdb, the UID will be different.
func (v *VerticaDB) GetPVSubPath(subPath string) string {
	return fmt.Sprintf("%s/%s", v.UID, subPath)
}

// GetDBDataPath get the data path for the current database
func (v *VerticaDB) GetDBDataPath() string {
	return fmt.Sprintf("%s/%s", v.Spec.Local.DataPath, v.Spec.DBName)
}

// GetCommunalPath returns the path to use for communal storage
func (v *VerticaDB) GetCommunalPath() string {
	// We include the UID in the communal path to generate a unique path for
	// each new instance of vdb. This means we can't use the same base path for
	// different databases and we don't require any cleanup if the vdb was
	// recreated.
	if !v.Spec.Communal.IncludeUIDInPath {
		return v.Spec.Communal.Path
	}
	return fmt.Sprintf("%s/%s", v.Spec.Communal.Path, v.UID)
}

func (v *VerticaDB) GetDepotPath() string {
	return fmt.Sprintf("%s/%s", v.Spec.Local.DepotPath, v.Spec.DBName)
}
