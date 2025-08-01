/*
 (c) Copyright [2021-2024] Open Text.
 Licensed under the Apache License, Version 2.0 (the "License");
 You may not use this file except in compliance with the License.
 You may obtain a copy of the License at

 http://www.apache.org/licenses/LICENSE-2.0

 Unless required by applicable law or agreed to in writing, software
 distributed under the License is distributed on an "AS IS" BASIS,
 WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 See the License for the specific language governing permissions and
 limitations under the License.
*/

package meta

import (
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	// Annotations that we set in each of the pod.  These are set by the
	// AnnotateAndLabelPodReconciler.  They are available in the pod with the
	// downwardAPI so they can be picked up by the Vertica data collector (DC).
	KubernetesVersionAnnotation   = "kubernetes.io/version"   // Version of the k8s server
	KubernetesGitCommitAnnotation = "kubernetes.io/gitcommit" // Git commit of the k8s server
	KubernetesBuildDateAnnotation = "kubernetes.io/buildDate" // Build date of the k8s server

	// A status annotation that shows the number of ready vertica hosts to the
	// number of total hosts
	ReadyStatusAnnotation = "vertica.com/ready-status"

	// If this annotation is on any CR, the operator will skip processing. This can
	// be used to avoid getting in an infinity error-retry loop. Or, if you know
	// no additional work will ever exist for an object. Just set this to a
	// true|ON|1 value.
	PauseOperatorAnnotation = "vertica.com/pause"

	// This is a feature flag for using vertica without admintools. Set this
	// annotation in the VerticaDB that you want to use the new vclusterOps
	// library for any vertica admin task. The value of this annotation is
	// treated as a boolean.
	VClusterOpsAnnotation      = "vertica.com/vcluster-ops"
	VClusterOpsAnnotationTrue  = "true"
	VClusterOpsAnnotationFalse = "false"

	// This is a feature flag for using vertica client proxy. Set this
	// annotation in the VerticaDB that you want to start proxy pods to
	// redirect the subcluster connections. The value of this annotation is
	// treated as a boolean.
	UseVProxyAnnotation      = "vertica.com/use-client-proxy"
	UseVProxyAnnotationTrue  = "true"
	UseVProxyAnnotationFalse = "false"

	// This is the log level of the proxy server will have.
	// Log level: TRACE|DEBUG|INFO|WARN|FATAL|NONE
	VProxyLogLevelAnnotation   = "vertica.com/client-proxy-log-level"
	VProxyLogLevelDefaultLevel = "INFO"

	// This is a feature flag for mounting vproxy certs as a secret volume in server containerss.
	// When set to true the vproxy reads certs from this mounted volume,
	// when set to false it reads certs directly from k8s secret store.
	MountVProxyCertsAnnotation      = "vertica.com/mount-vproxy-certs"
	MountVProxyCertsAnnotationTrue  = "true"
	MountVProxyCertsAnnotationFalse = "false"

	// This is a feature flag for mounting NMA certs as a secret volume in server containers
	// if deployment method is vclusterops. When set to true the NMA reads certs from this mounted
	// volume, when set to false it reads certs directly from k8s secret store.
	MountNMACertsAnnotation      = "vertica.com/mount-nma-certs"
	MountNMACertsAnnotationTrue  = "true"
	MountNMACertsAnnotationFalse = "false"

	// This is a feature flag for enables authentication via Mutual TLS
	EnableTLSAuthAnnotation = "vertica.com/enable-tls-auth"

	// Two annotations that are set by the operator when creating objects.
	OperatorDeploymentMethodAnnotation = "vertica.com/operator-deployment-method"
	OperatorVersionAnnotation          = "vertica.com/operator-version"

	// Ignore the cluster lease when doing a revive or start_db.  Use this with
	// caution, as ignoring the cluster lease when another system is using the
	// same communal storage will cause corruption.
	IgnoreClusterLeaseAnnotation = "vertica.com/ignore-cluster-lease"

	// When set to False, this parameter will ensure that when changing the
	// vertica version that we follow the upgrade path.  The Vertica upgrade
	// path means you cannot downgrade a Vertica release.
	IgnoreUpgradePathAnnotation     = "vertica.com/ignore-upgrade-path"
	IgnoreUpgradePathAnntationTrue  = "true"
	IgnoreUpgradePathAnntationFalse = "false"

	// The timeout, in seconds, to use when the operator restarts a node or the
	// entire cluster.  If omitted, we use the default timeout of 20 minutes.
	RestartTimeoutAnnotation = "vertica.com/restart-timeout"

	// The timeout, in seconds, to use when the operator creates a db and
	// waits for its startup.  If omitted, we use the default timeout of 5 minutes.
	CreateDBTimeoutAnnotation = "vertica.com/createdb-timeout"

	// The time in seconds to wait for a subcluster or database users' disconnection, its default value is 60.
	// This is a leagacy code as we used to use this to control draining timeout during stop db and stop subcluster.
	// Now there is a single annotation "vertica.com/active-connections-drain-seconds" to drain users' connections.
	ShutdownDrainSecondsAnnotation = "vertica.com/shutdown-drain-seconds"
	ShutdownDefaultDrainSeconds    = 60

	// The time at which draining pending delete pods has started. When greater than'vertica.com/active-connection-drain-seconds'
	// it means the timeout has expired and all active connections will be closed.
	DrainStartAnnotation = "vertica.com/drain-start-time"

	// When draining for read-only upgrade, we want to drain per subcluster. The time at which a particular subcluster
	// started draining will be stored in a specific annotation, such as 'vertica.com/drain-start-time-sc1'. This is the
	// prefix of that annotation. Functions similar to drain-start-time above
	DrainStartSubclusterPrefixAnnotation = DrainStartAnnotation

	// The time in seconds to wait for a subcluster or database users' disconnection, its default value is 60
	ActiveConnectionsDrainSecondsAnnotation = "vertica.com/active-connections-drain-seconds"
	ActiveConnectionsDefaultDrainSeconds    = 60

	// The timeout, in seconds, to use when the operator is performing online upgrade
	// for various tasks. If omitted, we use the default timeout of 5 minutes.
	OnlineUpgradeTimeoutAnnotation = "vertica.com/online-upgrade-timeout"
	OnlineUpgradeDefaultTimeout    = 5 * 60

	// Sets the fault tolerance for the cluster.  Allowable values are 0 or 1.  0 is only
	// suitable for test environments because we have no fault tolerance and the cluster
	// can only have between 1 and 3 pods.  If set to 1, which is the default,
	// we have fault tolerance if nodes die and the cluster has a minimum of 3
	// pods.  This value is only used during bootstrap of the VerticaDB.
	KSafetyAnnotation   = "vertica.com/k-safety"
	KSafetyDefaultValue = "1"

	// When enabled, the webhook will validate k-safety based on the number of
	// primary nodes in the cluster. Otherwise, validation will be based on the
	// total number of nodes in the cluster. The correct way is to use only the
	// primary nodes. This annotation exists only because the k-safety
	// validation in the v1beta1 API was implemented incorrectly. The v1 API
	// does it correctly. We can remove this annotation once the v1beta1 API is
	// no longer supported.
	StrictKSafetyCheckAnnotation = "vertica.com/strict-k-safety-check"

	// If a reconciliation iteration needs to be requeued this controls the
	// amount of time in seconds to wait.  If this is set to 0, or not set, then
	// the requeue time will increase using an exponential backoff algorithm.
	// Caution, when setting this to some positive value the exponential backoff
	// is disabled.  This should be reserved for test environments as an error
	// scenario could easily consume the logs.
	RequeueTimeAnnotation = "vertica.com/requeue-time"

	// If a reconciliation iteration during an operation such as Upgrade needs
	// to be requeued, this controls the amount of time in seconds to delay
	// adding the key to the reconcile queue.  If the RequeueTimeAnnotation is
	// set, it overrides this value.  If RequeueTimeAnnotation is not set
	// either, then we set the default value only for upgrades. For other
	// reconciles we use the exponential backoff algorithm.
	UpgradeRequeueTimeAnnotation = "vertica.com/upgrade-requeue-time"

	// Annotation to disable depot volume management and let it be provided by
	// a volumeMount for example. Useful for test environments and when using
	// Ephemeral PVCs
	NoDepotVolumeManagementAnnotation      = "vertica.com/disable-depot-volume-management"
	NoDepotVolumeManagementAnnotationTrue  = "true"
	NoDepotVolumeManagementAnnotationFalse = "false"

	// Annotation to disable auto-mounting of extraPaths to local volumes
	// as this behavior interferes with manual volume management
	NoExtraPathsAutoMountAnnotation   = "vertica.com/disable-extra-paths-auto-mount"
	NoExtraPathsAutoMountDefaultFalse = false

	// A secret that has the files for /home/dbadmin/.ssh.  If this is
	// omitted, the ssh files from the image are used (if applicable). SSH is
	// only required when deploying via admintools and is present only in images
	// tailored for that deployment type.  You can use this option if you have a
	// cluster that talks to Vertica notes outside of Kubernetes, as it has the
	// public keys to be able to ssh to those nodes.  It must have the following
	// keys present: id_rsa, id_rsa.pub and authorized_keys.
	SSHSecAnnotation = "vertica.com/ssh-secret"

	// If true, the operator will include the VerticaDB's UID in the path.  This
	// option exists if you reuse the communal path in the same endpoint as it
	// forces each database path to be unique.
	IncludeUIDInPathAnnotation = "vertica.com/include-uid-in-path"

	// Annotations that we add by parsing vertica --version output
	VersionAnnotation   = "vertica.com/version"
	BuildDateAnnotation = "vertica.com/buildDate"
	BuildRefAnnotation  = "vertica.com/buildRef"
	// Annotation that records the vertica version prior to the upgrade
	PreviousVersionAnnotation = "vertica.com/previous-version"
	// Annotation for the database's revive_instance_id
	ReviveInstanceIDAnnotation = "vertica.com/revive-instance-id"

	// Annotation for a customized superuser name. This annotation can be used
	// when vclusterops annotation is set to true. It can explicitly specify the
	// name of vertica superuser that is generated in database creation. If this
	// annotation is not provided the default value "dbadmin" will be used.
	SuperuserNameAnnotation   = "vertica.com/superuser-name"
	SuperuserNameDefaultValue = "dbadmin"

	// Annotation to control the termination grace period for vertica pods.
	TerminationGracePeriodSecondsAnnotaton = "vertica.com/termination-grace-period-seconds"

	// During the create database process, if we discover that Vertica is
	// already running, we can either treat this as an error or continue.
	// Continuing may be the best option if we have already progressed far
	// enough in the create database process to have created the catalog. We can
	// then continue to start any nodes that may be down in order to bring the
	// cluster online.
	FailCreateDBIfVerticaIsRunningAnnotation      = "vertica.com/fail-create-db-if-vertica-is-running"
	FailCreateDBIfVerticaIsRunningAnnotationTrue  = "true"
	FailCreateDBIfVerticaIsRunningAnnotationFalse = "false"

	// This is an annotation that controls the generation of httpstls.json. This
	// config file may control the startup of the embedded HTTPS service -- only
	// if TLS auth configuration is not set up in the Vertica catalog.  For
	// databases that were created in recent versions (24.1.0+) or if the TLS
	// auth configuration is set up in the catalog, this config file isn't
	// needed. The HTTPS service is required for vclusterOps deployments.
	HTTPSTLSConfGenerationAnnotation      = "vertica.com/https-tls-conf-generation"
	HTTPSTLSConfGenerationAnnotationTrue  = "true"
	HTTPSTLSConfGenerationAnnotationFalse = "false"
	HTTPSTLSConfGenerationDefaultValue    = true

	// This annotation disables TLS rollback functionality. Setting this ensures
	// backwards compatibility with functionality for versions <25.4.0. Default is
	// currently false (disabling this feature).
	DisableTLSRotationFailureRollbackAnnotation      = "vertica.com/disable-tls-rotation-failure-rollback"
	DisableTLSRotationFailureRollbackAnnotationTrue  = "true"
	DisableTLSRotationFailureRollbackAnnotationFalse = "false"
	DisableTLSRotationFailureRollbackDefaultValue    = true

	// This annotation forces a failure of the next TLS update cert rotation. There
	// are two places where this can be forced:
	//   "before_tls_update": fail before the secret has been updated in the DB
	//   "after_tls_update": fail before the secret has been updated in the DB
	// This annotation is internal only and should only be used for testing the
	// rollback after failed cert rotation functionality
	TriggerTLSUpdateFailureAnnotation      = "vertica.com/trigger-tls-update-failure"
	TriggerTLSUpdateFailureBeforeTLSUpdate = "before_tls_update"
	TriggerTLSUpdateFailureAfterTLSUpdate  = "after_tls_update"

	// We have a deployment check that ensures that if running vcluster ops the
	// image is built for that (and vice-versa). This annotation allows you to
	// skip that check.
	SkipDeploymentCheckAnnotation = "vertica.com/skip-deployment-check"

	// Set of annotations that you can use to control the resources of the NMA
	// sidecar. The actual annotation name is:
	//   vertica.com/nma-resources-<limits|requests>-<memory|cpu>
	//
	// For example, the following are valid:
	//   vertica.com/nma-resources-limits-memory
	//   vertica.com/nma-resources-limits-cpu
	//   vertica.com/nma-resources-requests-memory
	//   vertica.com/nma-resources-requests-cpu
	//
	// You can use GenNMAResourcesAnnotationName to generate the name.
	//
	// If the annotation is set, but has no value, than that resource is not
	// used. If a value is specified, but isn't able to be parsed, we use the
	// default.
	NMAResourcesPrefixAnnotation = "vertica.com/nma-resources"

	// Normally the nma sidecar resources are only applied if the corresponding
	// resource is set for the server container. This is done so that we can
	// avoid setting resources if they are left off of the server. This allows
	// us to run in low-resource environment. For those that don't want this
	// behavior, but instead want the NMA sidecar resource set, you can set
	// this annotation to true.
	NMAResourcesForcedAnnotation = "vertica.com/nma-resources-forced"

	// Set of annotations to control various settings with the health probes.
	// The format is:
	//   vertica.com/nma-<probe-name>-probe-<field-name>
	//
	// Where <probe-name> is one of:
	NMAHealthProbeReadiness = "readiness"
	NMAHealthProbeStartup   = "startup"
	NMAHealthProbeLiveness  = "liveness"
	// <field-name> is one of:
	NMAHealthProbeSuccessThreshold    = "success-threshold"
	NMAHealthProbeFailureThreshold    = "failure-threshold"
	NMAHealthProbePeriodSeconds       = "period-seconds"
	NMAHealthProbeTimeoutSeconds      = "timeout-seconds"
	NMAHealthProbeInitialDelaySeconds = "initial-delay-seconds"
	//
	// Use GenNMAHealthProbeAnnotationName to generate the name.

	// Set of annotations for scrutinize

	// Controls how long the scrutinize pod will keep running. The time is specified in seconds.
	ScrutinizePodTTLAnnotation   = "vertica.com/scrutinize-pod-ttl"
	ScrutinizePodTTLDefaultValue = 1800 // 30min
	// Allows you to control the restartPolicy of the scrutinize pod.
	ScrutinizePodRestartPolicyAnnotation = "vertica.com/scrutinize-pod-restart-policy"
	// The scrutinize pod has an init container, that runs vcluster scrutinize command,
	// and a main container that stays alive for a period of time to allow you to access
	// the tarball generated by scrutinize.
	// This controls the docker image of that main container.
	ScrutinizeMainContainerImageAnnotation   = "vertica.com/scrutinize-main-container-image"
	ScrutinizeMainContainerImageDefaultValue = "rockylinux:8"
	// Set of annotations that you can use to control the resources of the scrutinize
	// main container. The actual annotation name is:
	//   vertica.com/scrutinize-main-container-resources-<limits|requests>-<memory|cpu>
	//
	// For example, the following are valid:
	//   vertica.com/scrutinize-main-container-resources-limits-memory
	//   vertica.com/scrutinize-main-container-resources-limits-cpu
	//   vertica.com/scrutinize-main-container-resources-requests-memory
	//   vertica.com/scrutinize-main-container-resources-requests-cpu
	//
	// You can use GenScrutinizeMainContainerResourcesAnnotationName to generate the name.
	//
	// If the annotation is set, but has no value, then that resource is not
	// used. If a value is specified, but isn't able to be parsed, we use the
	// default.
	ScrutinizeMainContainerResourcesPrefixAnnotation = "vertica.com/scrutinize-main-container-resources"

	// In order to facilitate diagnosing less recent problems, scrutinize
	// should be able to collect an arbitrary time range of logs.
	// With the oldest time param or log age set, no archives of vertica.log
	// should be older than that time.
	ScrutinizeLogAgeOldestTime = "vertica.com/scrutinize-log-age-oldest-time"
	ScrutinizeLogAgeNewestTime = "vertica.com/scrutinize-log-age-newest-time"
	// The hours param cannot be set alongside the Time options, and if
	// attempted, should issue an error indicating so.
	ScrutinizeLogAgeHours = "vertica.com/scrutinize-log-age-hours"

	// This is applied to the statefulset to identify what replica group it is
	// in. Replica groups are assigned during online upgrade. Valid values
	// are defined under the annotation name.
	ReplicaGroupAnnotation = "vertica.com/replica-group"
	ReplicaGroupAValue     = "a"
	ReplicaGroupBValue     = "b"

	// For each subcluster that is included in either replica group, there is a
	// subcluster in the other replica group. This annotation is used to
	// establish the relationship.
	ParentSubclusterAnnotation = "vertica.com/parent-subcluster"
	// For each subcluster in replica group b, this is type of the associated
	// subcluster in replica group a.
	ParentSubclusterTypeAnnotation = "vertica.com/parent-subcluster-type"

	// During online upgrade, we store an annotation in the VerticaDB that
	// is the name of the sandbox for all subclusters part of replica group B.
	OnlineUpgradeSandboxAnnotation = "vertica.com/online-upgrade-sandbox"

	// This is the name of the VerticaReplicator that is generated during a online upgrade
	OnlineUpgradeReplicatorAnnotation = "vertica.com/online-upgrade-replicator-name"

	// During online upgrade, this annotation store some steps that we have
	// already passed. This will allow us to skip them.
	OnlineUpgradeStepInxAnnotation = "vertica.com/online-upgrade-step-index"

	// During online upgrade, we store an annotation in the VerticaDB to indicate
	// that we have removed old-main-cluster/replica-group-A.
	OnlineUpgradeReplicaARemovedAnnotation = "vertica.com/online-upgrade-replica-A-removed"
	ReplicaARemovedTrue                    = "true"
	ReplicaARemovedFalse                   = "false"

	// The sandbox name used for online upgrade contains a uuid. This annotation
	// will allow to set a fixed name for testing purposes
	OnlineUpgradePreferredSandboxAnnotation = "vertica.com/online-upgrade-preferred-sandbox"

	// This indicates the number of times we have tryied sandbox promotion during online
	// upgrade. The max number of attempts is 3 and after that we fail online upgrade.
	OnlineUpgradePromotionAttemptAnnotation = "vertica.com/online-upgrade-promotion-attempt"
	OnlineUpgradePromotionMaxAttempts       = 3

	// Allows us to set the name of the archive before replication for testing purposes.
	OnlineUpgradeArchiveBeforeReplicationAnnotation = "vertica.com/online-upgrade-archive-before-replication"

	SaveRestorePointAnnotation = "vertica.com/save-restore-point-on-upgrade"

	// This will be set in a sandbox configMap by the vdb controller to wake up the sandbox
	// controller for upgrading the sandboxes
	SandboxControllerUpgradeTriggerID = "vertica.com/sandbox-controller-upgrade-trigger-id"
	// This will be set in a sandbox configMap by the vdb controller to wake up the sandbox
	// controller for unsandboxing the subclusters
	SandboxControllerUnsandboxTriggerID = "vertica.com/sandbox-controller-unsandbox-trigger-id"
	// This will  be set in a sandbox configMap by the vdb controller to wake up the sandbox
	// controller for stopping/starting a sandbox
	SandboxControllerShutdownTriggerID = "vertica.com/sandbox-controller-shutdown-trigger-id"
	// This will  be set in a sandbox configMap by the vdb controller to wake up the sandbox
	// controller for alter subcluster type in a sandbox
	SandboxControllerAlterSubclusterTypeTriggerID = "vertica.com/sandbox-controller-alter-subcluster-type-trigger-id"

	// Use this to override the name of the statefulset and its pods. This needs
	// to be set in the spec.subclusters[].annotations field to take effect. If
	// omitted, then the name of the subclusters' statefulset will be
	// `<vdb-name>-<subcluster-name>'
	StsNameOverrideAnnotation = "vertica.com/statefulset-name-override"

	// Use this to override the name of the proxy deployment and its pods. This needs
	// to be set in the spec.subclusters[].annotations field to take effect. If
	// omitted, then the name of the subclusters' proxy will be
	// `<vdb-name>-<subcluster-name>-proxy'
	ProxyDeploymentNameAnnotation = "vertica.com/deployment-name-override"

	// Use this to store extra local paths that we need to create before revive_db.
	// Those paths include local paths not in local.catalogPath, local.dataPath,
	// and local.depotPath. For example, the user-created temp paths.
	ExtraLocalPathsAnnotation = "vertica.com/extra-local-paths"

	// This indicates that the subcluster shutdown is controlled by the sandbox
	// through the sandbox's shutdown field.
	ShutdownDrivenBySandbox = "vertica.com/shutdown-driven-by-sandbox"

	// This indicates that the subcluster shutdown is controlled by the subcluster's
	// shutdown field.
	ShutdownDrivenBySubcluster = "vertica.com/shutdown-driven-by-subcluster"

	// The timeout, in seconds, to use when the operator is polling the status of an ongoing
	// asynchronous replication operation. If omitted, we use the default timeout of 60 minutes.
	ReplicationTimeoutAnnotation          = "vertica.com/replication-timeout"
	ReplicationDefaultTimeout             = 60 * 60
	ReplicationPollingFrequencyAnnotation = "vertica.com/replication-polling-frequency"
	ReplicationDefaultPollingFrequency    = 5

	// Annotation set in a sandbox configMap. Indicates that routing must be disabled
	// on the sandbox nodes.
	DisableRoutingAnnotation = "vertica.com/disable-routing"

	// It will pause scaling immediately and use the current instance count.
	// This only works for a scaledobject.
	PausingAutoscalingAnnotation = "vertica.com/paused-scaling"

	// It will scale your current workload to specified amount of replicas and pause autoscaling.
	// You can set the value of replicas for an object to be paused to any arbitrary number.
	// This only works for a scaledobject.
	PausingAutoscalingReplicasAnnotation = "vertica.com/paused-scaling-replicas"

	// It will disable fetch_node_details log info. This makes debugging easier.
	DisableFetchNodeDetailsInfoLog = "vertica.com/disable-fetch-node-details-log-info"

	// This annotation ensures the database directory is preserved after the VDB is removed.
	// As a result, when the user revives the old database, the directory still exists,
	// allowing us to retain the DC tables.
	// This is currently used internally for K8s stress test.
	PreserveDBDirectoryAnnotation = "vertica.com/preserve-db-dir"

	// This annotation controls how long the TLS cache should be kept.
	TLSCacheDurationAnnotation = "vertica.com/tls-cache-duration"
	TLSCacheDefaultDuration    = 1 * 24 * 3600 // 1 day

	// This annotation ensures the tls secrets are removed after the VDB is removed.
	RemoveTLSSecretOnVDBDeleteAnnotation = "vertica.com/remove-tls-secret-on-vdb-delete" // #nosec G101

	// Interval (in seconds) at which Prometheus scrapes the metrics from the target.
	// If empty, Prometheus uses the global scrape interval.
	PrometheusScrapeIntervalAnnotation = "vertica.com/prometheus-scrape-interval"
)

// IsPauseAnnotationSet will check the annotations for a special value that will
// pause the operator for the CR.
func IsPauseAnnotationSet(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, PauseOperatorAnnotation, false /* default value */)
}

// UseVClusterOps returns true if all admin commands should use the vclusterOps
// library rather than admintools.
func UseVClusterOps(annotations map[string]string) bool {
	// UseVClusterOps returns true if the annotation isn't set.
	return lookupBoolAnnotation(annotations, VClusterOpsAnnotation, true /* default value */)
}

// UseVProxy returns true if all subcluster connections redirect to the proxy pods
func UseVProxy(annotations map[string]string) bool {
	// UseVProxy returns false if the annotation isn't set.
	return lookupBoolAnnotation(annotations, UseVProxyAnnotation, false /* default value */)
}

// UseVProxyCertsMount returns true if the proxy reads certs from the mounted secret
// volume rather than directly from k8s secret store.
func UseVProxyCertsMount(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, MountVProxyCertsAnnotation, true /* default value */)
}

// UseNMACertsMount returns true if the NMA reads certs from the mounted secret
// volume rather than directly from k8s secret store.
func UseNMACertsMount(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, MountNMACertsAnnotation, false /* default value */)
}

func UseTLSAuth(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, EnableTLSAuthAnnotation, false /* default value */)
}

// IgnoreClusterLease returns true if revive/start should ignore the cluster lease
func IgnoreClusterLease(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IgnoreClusterLeaseAnnotation, false /* default value */)
}

// IgnoreUpgradePath returns true if the upgrade path can be ignored when
// changing images.
func IgnoreUpgradePath(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IgnoreUpgradePathAnnotation, false /* default value */)
}

// GetRestartTimeout returns the timeout to use for restart node or start db. If
// 0 is returned, this means to use the default.
func GetRestartTimeout(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, RestartTimeoutAnnotation, 0 /* default value */)
}

// GetCreateDBNodeStartTimeout returns the timeout to use for create db node startup. If
// 0 is returned, this means to use the default.
func GetCreateDBNodeStartTimeout(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, CreateDBTimeoutAnnotation, 0 /* default value */)
}

// getShutdownDrainSeconds returns the time in seconds to wait for a subcluster/database users' disconnection
func getShutdownDrainSeconds(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, ShutdownDrainSecondsAnnotation, ShutdownDefaultDrainSeconds /* default value */)
}

// GetActiveConnectionsDrainSeconds returns the time in seconds to wait for a subcluster/database users' disconnection.
// It checks the ActiveConnectionsDrainSeconds annotation first,
// falling back to ShutdownDrainSeconds if not set, and finally uses the default.
func GetActiveConnectionsDrainSeconds(annotations map[string]string) int {
	if _, found := annotations[ActiveConnectionsDrainSecondsAnnotation]; found {
		return lookupIntAnnotation(annotations, ActiveConnectionsDrainSecondsAnnotation, ActiveConnectionsDefaultDrainSeconds /* default value */)
	}
	if _, found := annotations[ShutdownDrainSecondsAnnotation]; found {
		return getShutdownDrainSeconds(annotations)
	}
	return ActiveConnectionsDefaultDrainSeconds
}

// GetOnlineUpgradeTimeout returns the timeout to use for pause/redirect sessions
func GetOnlineUpgradeTimeout(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, OnlineUpgradeTimeoutAnnotation, OnlineUpgradeDefaultTimeout)
}

// IsKSafety0 returns true if k-safety is set to 0. False implies 1.
func IsKSafety0(annotations map[string]string) bool {
	return lookupStringAnnotation(annotations, KSafetyAnnotation, KSafetyDefaultValue) == "0"
}

// GetRequeueTime returns the amount of seconds to wait between reconciliation
// that are requeued. 0 means use the exponential backoff algorithm.
func GetRequeueTime(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, RequeueTimeAnnotation, 0 /* default value */)
}

// GetUpgradeRequeueTime returns the amount of seconds to wait between
// reconciliations during an upgrade.
func GetUpgradeRequeueTime(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, UpgradeRequeueTimeAnnotation, 0 /* default value */)
}

// GetSSHSecretName returns the name of the secret that contains SSH keys to use
// for admintools style of deployments.
func GetSSHSecretName(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, SSHSecAnnotation, "")
}

// IncludeUIDInPath will return true if the UID should be included in the
// communal path to make it unique.
func IncludeUIDInPath(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IncludeUIDInPathAnnotation, false /* default value */)
}

// GetSuperuserName returns the name of customized vertica superuser name
// for vclusterops style of deployments.
func GetSuperuserName(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, SuperuserNameAnnotation, SuperuserNameDefaultValue)
}

// IsKSafetyCheckStrict returns whether the k-safety check is relaxed.
// If false (default value), the webhook will calculate the k-safety value
// based on the number of primary nodes in the cluster;
// if true, the calculation will be based on the number of all nodes
// in the cluster.
func IsKSafetyCheckStrict(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, StrictKSafetyCheckAnnotation, true /* default value */)
}

// GetTerminationGracePeriodSeconds returns the value we will use for
// termination grace period in vertica pods. This is the amount of time k8s will
// wait before forcibly removing the pod.
func GetTerminationGracePeriodSeconds(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, TerminationGracePeriodSecondsAnnotaton, 0 /* default value */)
}

// FailCreateDBIfVerticaIsRunning is used to see how to handle failures during create
// db if vertica is found to be running. It returns true if an error indicating
// vertica is running should be ignored.
func FailCreateDBIfVerticaIsRunning(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, FailCreateDBIfVerticaIsRunningAnnotation, false /* default value */)
}

// IsHTTPSTLSConfGenerationAnnotatonSet returns true if the annotaton controlling
// the httpstls.json file generation is set.
func IsHTTPSTLSConfGenerationAnnotationSet(annotations map[string]string) bool {
	_, ok := annotations[HTTPSTLSConfGenerationAnnotation]
	return ok
}

// IsHTTPSTLSConfGenerationEnabled is true if the httpstls.json file should be
// generated by the operator. This file may control if the HTTPS service starts;
// depends on TLS auth config in the catalog.
func IsHTTPSTLSConfGenerationEnabled(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, HTTPSTLSConfGenerationAnnotation, HTTPSTLSConfGenerationDefaultValue)
}

// IsDisableTLSRollbackAnnotationSet returns true if DisableTLSFailureRollbackAnnotation is set,
// disabling TLS cert rollback after failed rotation
func IsDisableTLSRollbackAnnotationSet(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, DisableTLSRotationFailureRollbackAnnotation,
		DisableTLSRotationFailureRollbackDefaultValue)
}

// GetTriggerTLSUpdateFailureAnnotation returns the string value of the annotation TriggerTLSUpdateFailureAnnotation,
// which is used as a backdoor to trigger cert rotation failures, in order to test rollback
func GetTriggerTLSUpdateFailureAnnotation(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, TriggerTLSUpdateFailureAnnotation, "")
}

// GetSkipDeploymentCheck will return true if we are to skip the check that
// ensures the deployment method picked (vcluster or admintools) matches what
// the image was built for.
func GetSkipDeploymentCheck(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, SkipDeploymentCheckAnnotation, false /* default value */)
}

// DisableDepotVolumeManagement will return true if we should not manage the depot volume
// in the operator, allowing different provisioning mechanisms
func DisableDepotVolumeManagement(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, NoDepotVolumeManagementAnnotation, false)
}

// DisableExtraPathsAutoMount will return true if we should not auto-mount the extra paths
// in the operator, allowing different provisioning mechanisms (such as manual mounts)
func DisableExtraPathsAutoMount(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, NoExtraPathsAutoMountAnnotation, NoExtraPathsAutoMountDefaultFalse)
}

// GetNMAResource is used to retrieve a specific resource for the NMA
// sidecar. If any parsing error occurs, the default value is returned.
func GetNMAResource(annotations map[string]string, resourceName corev1.ResourceName) resource.Quantity {
	annotationName := GenNMAResourcesAnnotationName(resourceName)
	defVal, hasDefault := DefaultNMAResources[resourceName]
	defValStr := defVal.String()
	if !hasDefault {
		defValStr = ""
	}
	return getResource(annotations, annotationName, defValStr, defVal)
}

// IsNMAResourcesForced returns true if the resources for the NMA
// sidecar should be set regardless if resources are set for the server. False
// means they should only be applyied if the corresponding resource is set in
// the server.
func IsNMAResourcesForced(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, NMAResourcesForcedAnnotation, false /* default value */)
}

// GenNMAResourcesAnnotationName is a helper to generate the name of the
// annotation to control the resource for NMA. The resourceName given is taken from the
// k8s corev1 package. It should be the two part name. Use const like
// corev1.ResourceLimitsCPU, corev1.ResourceRequestsMemory, etc.
func GenNMAResourcesAnnotationName(resourceName corev1.ResourceName) string {
	return genResourcesAnnotationName(NMAResourcesPrefixAnnotation, resourceName)
}

// GenNMAHealthProbeAnnotationName returns the name of the annotation for a specific health probe field.
func GenNMAHealthProbeAnnotationName(probeName, field string) string {
	return fmt.Sprintf("vertica.com/nma-%s-probe-%s", probeName, field)
}

// GetNMAHealthProbeOverride returns the value of a NMA health probe annotation.
// If the annotation isn't set, or its value doesn't convert to an int, then
// (0,false) is returned.
func GetNMAHealthProbeOverride(annotations map[string]string, probeName, field string) (int32, bool) {
	annName := GenNMAHealthProbeAnnotationName(probeName, field)
	annVal := lookupStringAnnotation(annotations, annName, "" /* default value */)
	if annVal == "" {
		return 0, false
	}
	convVal, err := strconv.Atoi(annVal)
	if err != nil {
		return 0, false
	}
	if convVal < 0 {
		return 0, false
	}
	return int32(convVal), true //nolint:gosec
}

// GetVProxyLogLevel returns scrutinize log age hours
func GetVProxyLogLevel(annotations map[string]string) string {
	return strings.ToUpper(lookupStringAnnotation(annotations, VProxyLogLevelAnnotation, VProxyLogLevelDefaultLevel))
}

// GetScrutinizePodTTL returns how long the scrutinize pod will keep running
func GetScrutinizePodTTL(annotations map[string]string) int {
	val := lookupIntAnnotation(annotations,
		ScrutinizePodTTLAnnotation, ScrutinizePodTTLDefaultValue)
	if val < 0 {
		return ScrutinizePodTTLDefaultValue
	}
	return val
}

// GetScrutinizePodRestartPolicy returns the scrutinize pod restart policy
func GetScrutinizePodRestartPolicy(annotations map[string]string) string {
	policy := lookupStringAnnotation(annotations, ScrutinizePodRestartPolicyAnnotation, string(corev1.RestartPolicyNever))
	if policy == string(corev1.RestartPolicyNever) ||
		policy == string(corev1.RestartPolicyAlways) ||
		policy == string(corev1.RestartPolicyOnFailure) {
		return policy
	}
	return string(corev1.RestartPolicyNever)
}

// GetScrutinizeMainContainerImage returns scrutinize main container image
func GetScrutinizeMainContainerImage(annotations map[string]string) string {
	img := lookupStringAnnotation(annotations, ScrutinizeMainContainerImageAnnotation,
		ScrutinizeMainContainerImageDefaultValue)
	if img == "" {
		return ScrutinizeMainContainerImageDefaultValue
	}
	return img
}

// GetScrutinizeMainContainerResource retrieves a specific resource for the scrutinize
// main container. If any parsing error occurs, the default value is returned.
func GetScrutinizeMainContainerResource(annotations map[string]string, resourceName corev1.ResourceName) resource.Quantity {
	annotationName := GenScrutinizeMainContainerResourcesAnnotationName(resourceName)
	defVal, hasDefault := DefaultScrutinizeMainContainerResources[resourceName]
	defValStr := defVal.String()
	if !hasDefault {
		defValStr = ""
	}
	return getResource(annotations, annotationName, defValStr, defVal)
}

// GenScrutinizeMainContainerResourcesAnnotationName is a helper to generate the name of the
// annotation to control the resource for scrutinize main container. The resourceName
// given is taken from the k8s corev1 package. It should be the two part name. Use const like
// corev1.ResourceLimitsCPU, corev1.ResourceRequestsMemory, etc.
func GenScrutinizeMainContainerResourcesAnnotationName(resourceName corev1.ResourceName) string {
	return genResourcesAnnotationName(ScrutinizeMainContainerResourcesPrefixAnnotation,
		resourceName)
}

// GetScrutinizeLogAgeOldestTime returns scrutinize log age oldest time
func GetScrutinizeLogAgeOldestTime(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, ScrutinizeLogAgeOldestTime, "" /* default value */)
}

// GetScrutinizeLogAgeNewestTime returns scrutinize log age newest time
func GetScrutinizeLogAgeNewestTime(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, ScrutinizeLogAgeNewestTime, "" /* default value */)
}

// GetScrutinizeLogAgeHours returns scrutinize log age hours
func GetScrutinizeLogAgeHours(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, ScrutinizeLogAgeHours, 0 /* default value */)
}

// GetOnlineUpgradeSandbox returns the name of the sandbox used for online upgrade.
func GetOnlineUpgradeSandbox(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, OnlineUpgradeSandboxAnnotation, "")
}

// GetOnlineUpgradeStepInx returns the online upgrade step we are in.
func GetOnlineUpgradeStepInx(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, OnlineUpgradeStepInxAnnotation, 0)
}

// GetOnlineUpgradeReplicaARemoved returns if replica A has been removed in online upgrade.
func GetOnlineUpgradeReplicaARemoved(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, OnlineUpgradeReplicaARemovedAnnotation, ReplicaARemovedFalse)
}

// GetOnlineUpgradeReplicator returns the name of the VerticaReplicator
// object used during online upgrade.
func GetOnlineUpgradeReplicator(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, OnlineUpgradeReplicatorAnnotation, "")
}

// GetOnlineUpgradePreferredSandboxName returns the sandbox name to use for online upgrade.
func GetOnlineUpgradePreferredSandboxName(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, OnlineUpgradePreferredSandboxAnnotation, "")
}

// GetOnlineUpgradePromotionAttempt returns the current number of promotion attempts
func GetOnlineUpgradePromotionAttempt(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, OnlineUpgradePromotionAttemptAnnotation, 0)
}

func GetOnlineUpgradeArchiveBeforeReplication(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, OnlineUpgradeArchiveBeforeReplicationAnnotation, "")
}

// GetSaveRestorePoint returns true if the operator must create
// restore points during upgrade
func GetSaveRestorePoint(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, SaveRestorePointAnnotation, false)
}

// GetStsNameOverride returns the override for the statefulset name. If one is
// not provided, an empty string is returned.
func GetStsNameOverride(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, StsNameOverrideAnnotation, "")
}

// GetVPDepNameOverride returns the override for the proxy deployment name.
// If one is not provided, an empty string is returned.
func GetVPDepNameOverride(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, ProxyDeploymentNameAnnotation, "")
}

func GetShutdownDrivenBySandbox(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, ShutdownDrivenBySandbox, false)
}

// GetShutdownDrivenBySubcluster returns the bool value if the operator
// will shutdown the subcluster and not try to restart it.
func GetShutdownDrivenBySubcluster(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, ShutdownDrivenBySubcluster, false)
}

// GetExtraLocalPaths returns the comma separated list of extra local paths
func GetExtraLocalPaths(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, ExtraLocalPathsAnnotation, "")
}

// GetReplicationTimeout returns the timeout (in seconds) to use for polling async replication status
func GetReplicationTimeout(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, ReplicationTimeoutAnnotation, ReplicationDefaultTimeout)
}

// GetReplicationPollingFrequency returns the frequency (in seconds) operator will poll async replication status
func GetReplicationPollingFrequency(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, ReplicationPollingFrequencyAnnotation, ReplicationDefaultPollingFrequency)
}

// GetDisableRouting returns true if routing must be disabled on the sandbox
// nodes.
func GetDisableRouting(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, DisableRoutingAnnotation, false)
}

// IsFetchNodeDetailsLogDisabled returns true if fetch node details vcluster api's
// log info must be disabled.
func IsFetchNodeDetailsLogDisabled(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, DisableFetchNodeDetailsInfoLog, false)
}

// GetPreserveDBDirectory returns true if the operator must preserve the DB directory
func GetPreserveDBDirectory(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, PreserveDBDirectoryAnnotation, false)
}

// GetTLSCacheDuration returns the duration (in seconds) to keep the TLS cache
func GetTLSCacheDuration(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, TLSCacheDurationAnnotation, TLSCacheDefaultDuration)
}

// ShouldRemoveTLSSecret returns true if a tls secret must be removed on VDB delete
func ShouldRemoveTLSSecret(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, RemoveTLSSecretOnVDBDeleteAnnotation, false)
}

func GetPrometheusScrapeInterval(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, PrometheusScrapeIntervalAnnotation, 0)
}

// lookupBoolAnnotation is a helper function to lookup a specific annotation and
// treat it as if it were a boolean.
func lookupBoolAnnotation(annotations map[string]string, annotation string, defaultValue bool) bool {
	if val, ok := annotations[annotation]; ok {
		varAsBool, err := strconv.ParseBool(val)
		if err != nil {
			return false
		}
		return varAsBool
	}
	return defaultValue
}

func lookupIntAnnotation(annotations map[string]string, annotation string, defaultValue int) int {
	if val, ok := annotations[annotation]; ok {
		varAsInt, err := strconv.ParseInt(val, 10, 0)
		if err != nil {
			return defaultValue
		}
		return int(varAsInt)
	}
	return defaultValue
}

func lookupStringAnnotation(annotations map[string]string, annotation, defaultValue string) string {
	if val, ok := annotations[annotation]; ok {
		return val
	}
	return defaultValue
}

// genResourcesAnnotationName is a helper to generate the name of the
// annotation to control a resource. The resourceName given is taken from the
// k8s corev1 package. It should be the two part name. Use const like
// corev1.ResourceLimitsCPU, corev1.ResourceRequestsMemory, etc.
func genResourcesAnnotationName(prefix string, resourceName corev1.ResourceName) string {
	// The resourceName pass in, taken from the corev1 k8s package, has the
	// resource name like "limits.cpu" or "requests.memory". We don't want the
	// period in the annotation name since it doesn't fit the style, so we
	// replace that with a dash.
	return genAnnotationName(prefix, strings.Replace(string(resourceName), ".", "-", 1))
}

// getResource retrieves a specific resource given the annotation.
// If any parsing error occurs, the default value is returned.
func getResource(annotations map[string]string, annotationName, defValStr string,
	defVal resource.Quantity) resource.Quantity {
	quantityStr := lookupStringAnnotation(annotations, annotationName, defValStr)
	// If the annotation is set, but has no value, then we will omit the
	// resource rather than use the default. This allows us to turn off the
	// resource if need be.
	if quantityStr == "" {
		return resource.Quantity{}
	}
	quantity, err := resource.ParseQuantity(quantityStr)
	if err != nil {
		return defVal
	}
	return quantity
}

func genAnnotationName(prefix, name string) string {
	return fmt.Sprintf("%s-%s", prefix, name)
}

// Find all annotation keys containing a certain prefix.
func findPrefixedAnnotations(annotations map[string]string, prefix string) ([]string, bool) {
	var matching []string
	for k := range annotations {
		if strings.HasPrefix(k, prefix) {
			matching = append(matching, k)
		}
	}
	return matching, len(matching) > 0
}

// Generate annotation name for subcluster-specific drain start time
// eg vertica.com/drain-start-time-sc1
func GenSubclusterDrainStartAnnotationName(scName string) string {
	return genAnnotationName(DrainStartSubclusterPrefixAnnotation, scName)
}

// Find any annotation names with the drain start prefix
// e.g., "vertica.com/drain-start-time-sc1"
func FindDrainTimeoutSubclusterAnnotations(annotations map[string]string) ([]string, bool) {
	return findPrefixedAnnotations(annotations, DrainStartSubclusterPrefixAnnotation)
}
