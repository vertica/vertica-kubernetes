/*
 (c) Copyright [2021-2023] Open Text.
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
	"strconv"
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

	// This is a feature flag for accessing the secrets configured in Google Secret Manager.
	// The value of this annotation is treated as a boolean.
	GcpGsmAnnotation = "vertica.com/use-gcp-secret-manager"

	// Two annotations that are set by the operator when creating objects.
	OperatorDeploymentMethodAnnotation = "vertica.com/operator-deployment-method"
	OperatorVersionAnnotation          = "vertica.com/operator-version"

	// Ignore the cluster lease when doing a revive or start_db.  Use this with
	// caution, as ignoring the cluster lease when another system is using the
	// same communal storage will cause corruption.
	IgnoreClusterLeaseAnnotation = "vertica.com/ignore-cluster-lease"

	// When set to False, this parameter will ensure that when changing the
	// vertica version that we follow the upgrade path.  The Vertica upgrade
	// path means you cannot downgrade a Vertica release, nor can you skip any
	// released Vertica versions when upgrading.
	IgnoreUpgradePathAnnotation = "vertica.com/ignore-upgrade-path"

	// The timeout, in seconds, to use when the operator restarts a node or the
	// entire cluster.  If omitted, we use the default timeout of 20 minutes.
	RestartTimeoutAnnotation = "vertica.com/restart-timeout"

	// Sets the fault tolerance for the cluster.  Allowable values are 0 or 1.  0 is only
	// suitable for test environments because we have no fault tolerance and the cluster
	// can only have between 1 and 3 pods.  If set to 1, which is the default,
	// we have fault tolerance if nodes die and the cluster has a minimum of 3
	// pods.  This value is only used during bootstrap of the VerticaDB.
	KSafetyAnnotation   = "vertica.com/k-safety"
	KSafetyDefaultValue = "1"

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
	// Annotation for the database's revive_instance_id
	ReviveInstanceIDAnnotation = "vertica.com/revive-instance-id"
)

// IsPauseAnnotationSet will check the annotations for a special value that will
// pause the operator for the CR.
func IsPauseAnnotationSet(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, PauseOperatorAnnotation)
}

// UseVClusterOps returns true if all admin commands should use the vclusterOps
// library rather than admintools.
func UseVClusterOps(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, VClusterOpsAnnotation)
}

// UseGCPSecretManager returns true if access to the communal secret should go through
// Google's secret manager rather the fetching the secret from k8s meta-data.
func UseGCPSecretManager(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, GcpGsmAnnotation)
}

// IgnoreClusterLease returns true if revive/start should ignore the cluster lease
func IgnoreClusterLease(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IgnoreClusterLeaseAnnotation)
}

// IgnoreUpgradePath returns true if the upgrade path can be ignored when
// changing images.
func IgnoreUpgradePath(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IgnoreUpgradePathAnnotation)
}

// GetRestartTimeout returns the timeout to use for restart node or start db. If
// 0 is returned, this means to use the default.
func GetRestartTimeout(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, RestartTimeoutAnnotation)
}

// IsKSafety0 returns true if k-safety is set to 0. False implies 1.
func IsKSafety0(annotations map[string]string) bool {
	return lookupStringAnnotation(annotations, KSafetyAnnotation, KSafetyDefaultValue) == "0"
}

// GetRequeueTime returns the amount of seconds to wait between reconciliation
// that are requeued. 0 means use the exponential backoff algorithm.
func GetRequeueTime(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, RequeueTimeAnnotation)
}

// GetUpgradeRequeueTime returns the amount of seconds to wait between
// reconciliations during an upgrade.
func GetUpgradeRequeueTime(annotations map[string]string) int {
	return lookupIntAnnotation(annotations, UpgradeRequeueTimeAnnotation)
}

// GetSSHSecretName returns the name of the secret that contains SSH keys to use
// for admintools style of deployments.
func GetSSHSecretName(annotations map[string]string) string {
	return lookupStringAnnotation(annotations, SSHSecAnnotation, "")
}

// IncludeUIDInPath will return true if the UID should be included in the
// communal path to make it unique.
func IncludeUIDInPath(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, IncludeUIDInPathAnnotation)
}

// lookupBoolAnnotation is a helper function to lookup a specific annotation and
// treat it as if it were a boolean.
func lookupBoolAnnotation(annotations map[string]string, annotation string) bool {
	defaultValue := false
	if val, ok := annotations[annotation]; ok {
		varAsBool, err := strconv.ParseBool(val)
		if err != nil {
			return defaultValue
		}
		return varAsBool
	}
	return defaultValue
}

func lookupIntAnnotation(annotations map[string]string, annotation string) int {
	const defaultValue = 0
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
