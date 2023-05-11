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

const (
	// Annotations that we set in each of the pod.  These are set by the
	// AnnotateAndLabelPodReconciler.  They are available in the pod with the
	// downwardAPI so they can be picked up by the Vertica data collector (DC).
	KubernetesVersionAnnotation   = "kubernetes.io/version"   // Version of the k8s server
	KubernetesGitCommitAnnotation = "kubernetes.io/gitcommit" // Git commit of the k8s server
	KubernetesBuildDateAnnotation = "kubernetes.io/buildDate" // Build date of the k8s server
)
