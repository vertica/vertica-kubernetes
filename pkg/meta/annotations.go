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

import "strconv"

const (
	// Annotations that we set in each of the pod.  These are set by the
	// AnnotateAndLabelPodReconciler.  They are available in the pod with the
	// downwardAPI so they can be picked up by the Vertica data collector (DC).
	KubernetesVersionAnnotation   = "kubernetes.io/version"   // Version of the k8s server
	KubernetesGitCommitAnnotation = "kubernetes.io/gitcommit" // Git commit of the k8s server
	KubernetesBuildDateAnnotation = "kubernetes.io/buildDate" // Build date of the k8s server

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
)

// IsPauseAnnotationSet will check the annotations for a special value that will
// pause the operator for the CR.
func IsPauseAnnotationSet(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, PauseOperatorAnnotation, false)
}

// UseVClusterOps returns true if all admin commands should use the vclusterOps
// library rather than admintools.
func UseVClusterOps(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, VClusterOpsAnnotation, false)
}

// UseGCPSecretManager returns true if access to the communal secret should go through
// Google's secret manager rather the fetching the secret from k8s meta-data.
func UseGCPSecretManager(annotations map[string]string) bool {
	return lookupBoolAnnotation(annotations, GcpGsmAnnotation, false)
}

// lookupBoolAnnotation is a helper function to lookup a specific annotation and
// treat it as if it were a boolean.
func lookupBoolAnnotation(annotations map[string]string, annotation string, defaultValue bool) bool {
	if val, ok := annotations[annotation]; ok {
		varAsBool, err := strconv.ParseBool(val)
		if err != nil {
			return defaultValue
		}
		return varAsBool
	}
	return defaultValue
}
