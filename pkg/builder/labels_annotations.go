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

package builder

import (
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/opcfg"
)

// MakeSubclusterLabels returns the labels added for the subcluster
func MakeSubclusterLabels(sc *vapi.Subcluster) map[string]string {
	m := map[string]string{
		vmeta.SubclusterNameLabel: sc.Name,
		vmeta.SubclusterTypeLabel: sc.GetType(),
	}
	return m
}

// MakeOperatorLabels returns the labels that all objects created by this operator will have
func MakeOperatorLabels(vdb *vapi.VerticaDB) map[string]string {
	return map[string]string{
		vmeta.ManagedByLabel:   vmeta.OperatorName,
		vmeta.VDBInstanceLabel: vdb.Name,
		vmeta.ComponentLabel:   vmeta.ComponentDatabase,
		vmeta.DataBaseLabel:    vdb.Spec.DBName,
	}
}

// MakeCommonLabels returns the labels that are common to all objects.
func MakeCommonLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster, forPod, forProxyPod bool) map[string]string {
	labels := MakeOperatorLabels(vdb)
	// This can be overridden through 'labels' in the CR.
	labels[vmeta.NameLabel] = vmeta.NameValue
	if forPod {
		labels[vmeta.SubclusterSelectorLabel] = sc.GetStatefulSetName(vdb)
		// Transient subclusters never have the service name label set.  At various
		// parts of the upgrade, it will accept traffic from all of the subclusters.
		if !sc.IsTransient() {
			labels[vmeta.SubclusterSvcNameLabel] = sc.GetServiceName()
		}
	} else {
		// Apply a label to indicate a version of the operator that created the
		// object.  This is separate from MakeOperatorLabels as we don't want to
		// set this for pods in the template.  We set the operator version in
		// the pods as part of a reconciler so that we don't have to reschedule
		// the pods.
		labels[vmeta.OperatorVersionLabel] = vmeta.CurOperatorVersion
	}

	if forProxyPod {
		appendProxyLabels(labels, vdb, sc)
		if !sc.IsTransient() {
			labels[vmeta.SubclusterSvcNameLabel] = sc.GetServiceName()
		}
	}

	// Labels for pods or for non-subcluster specific objects can stop here.
	// Pods don't have much of the subcluster labels because these can change
	// (e.g. subcluster rename) and so are hard to maintain in the pods.
	if sc == nil || forPod {
		return labels
	}

	for k, v := range MakeSubclusterLabels(sc) {
		labels[k] = v
	}

	return labels
}

// MakeLabelsForObjects constructs the labels for a new k8s object
func makeLabelsForObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, forPod, forProxyPod bool) map[string]string {
	labels := MakeCommonLabels(vdb, sc, forPod, forProxyPod)

	// Add any custom labels that were in the spec.
	for k, v := range vdb.Spec.Labels {
		labels[k] = v
	}

	return labels
}

// appendProxyLabels appends the proxy labels to an existing label set
func appendProxyLabels(labels map[string]string, vdb *vapi.VerticaDB, sc *vapi.Subcluster) {
	// Set a special selector to pick only the pods for this porxy deployment. It's
	// derived from the deployment name as that stays constant and is unique in
	// a namespace.
	labels[vmeta.DeploymentSelectorLabel] = vdb.GetVProxyDeploymentName(sc.Name)
	// Set the common proxy pod selector
	labels[vmeta.ProxyPodSelectorLabel] = vmeta.ProxyPodSelectorVal
}

// MakeLabelsForPodObject constructs the labels that are common for all pods
func MakeLabelsForPodObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	return makeLabelsForObject(vdb, sc, true, false)
}

// MakeLabelsForStsObject constructs the labels that are common for all statefulsets.
func MakeLabelsForStsObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	labels := makeLabelsForObject(vdb, sc, false, false)
	sandbox := vdb.GetSubclusterSandboxName(sc.Name)
	if sandbox != vapi.MainCluster {
		labels[vmeta.WatchedBySandboxLabel] = vmeta.WatchedBySandboxTrue
		labels[vmeta.SandboxNameLabel] = sandbox
	}
	return labels
}

// MakeLabelsForSvcObject will create the set of labels for use with service objects
func MakeLabelsForSvcObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, svcType string) map[string]string {
	labels := makeLabelsForObject(vdb, sc, false, false)
	labels[vmeta.SvcTypeLabel] = svcType
	return labels
}

// MakeLabelsForVProxyObject constructs the labels of the client proxy config map and pods
func MakeLabelsForVProxyObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, forProxyPod bool) map[string]string {
	labels := makeLabelsForObject(vdb, sc, false, forProxyPod)
	return labels
}

// MakeLabelsForSandboxConfigMap constructs the labels of the sandbox config map
func MakeLabelsForSandboxConfigMap(vdb *vapi.VerticaDB) map[string]string {
	labels := makeLabelsForObject(vdb, nil, false, false)
	labels[vmeta.WatchedBySandboxLabel] = vmeta.WatchedBySandboxTrue
	return labels
}

// MakeAnnotationsForObjects builds the list of annotations that are to be
// included on new objects.
func MakeAnnotationsForObject(vdb *vapi.VerticaDB) map[string]string {
	annotations := make(map[string]string, len(vdb.Spec.Annotations))
	// Surface operator config as annotations. This is picked up by the downward
	// API and surfaced as files for the server to collect in the
	// dc_kubernetes_events table.
	annotations[vmeta.OperatorDeploymentMethodAnnotation] = opcfg.GetDeploymentMethod()
	annotations[vmeta.OperatorVersionAnnotation] = opcfg.GetVersion()
	for k, v := range vdb.Spec.Annotations {
		annotations[k] = v
	}
	return annotations
}

// MakeAnnotationsForStsObject builds the list of annotations that are include
// in the statefulset for a subcluster.
func MakeAnnotationsForStsObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	annotations := MakeAnnotationsForObject(vdb)
	for k, v := range sc.Annotations {
		annotations[k] = v
	}
	return annotations
}

// MakeAnnotationsForVProxyObject builds the list of annotations that are included
// in the proxy config object.
func MakeAnnotationsForVProxyObject(vdb *vapi.VerticaDB) map[string]string {
	annotations := MakeAnnotationsForObject(vdb)
	if ver, ok := vdb.Annotations[vmeta.VersionAnnotation]; ok {
		annotations[vmeta.VersionAnnotation] = ver
	}
	return annotations
}

// MakeAnnotationsForSandboxConfigMap builds the list of annotations that are included
// in the sandbox config map.
func MakeAnnotationsForSandboxConfigMap(vdb *vapi.VerticaDB) map[string]string {
	annotations := MakeAnnotationsForObject(vdb)
	if ver, ok := vdb.Annotations[vmeta.VersionAnnotation]; ok {
		annotations[vmeta.VersionAnnotation] = ver
	}
	return annotations
}

// MakeSvcSelectorLabels returns the labels that are used for selectors in service objects.
func MakeBaseSvcSelectorLabels(vdb *vapi.VerticaDB) map[string]string {
	// We intentionally don't use the common labels because that includes things
	// specific to the operator version.  To allow the selector to work with
	// pods created from an older operator, we need to be more selective in the
	// labels we choose.
	return map[string]string{
		vmeta.VDBInstanceLabel: vdb.Name,
	}
}

// MakeSvcSelectorLabelsForServiceNameRouting will create the labels for when we
// want a service object to pick the pods based on the service name.  This
// allows us to combine multiple subcluster under a single service object.
func MakeSvcSelectorLabelsForServiceNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	m[vmeta.SubclusterSvcNameLabel] = sc.GetServiceName()
	// Only route to nodes that have verified they own at least one shard and
	// aren't pending delete
	m[vmeta.ClientRoutingLabel] = vmeta.ClientRoutingVal
	return m
}

// MakeSvcSelectorLabelsForSubclusterNameRouting will create the labels for when
// we want a service object to pick the pods based on the subcluster name.
func MakeSvcSelectorLabelsForSubclusterNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	// Routing is done using the subcluster's sts name rather than the service name.
	m[vmeta.SubclusterSelectorLabel] = sc.GetStatefulSetName(vdb)
	m[vmeta.ClientRoutingLabel] = vmeta.ClientRoutingVal

	return m
}

// MakeStsSelectorLabels will create the selector labels for use within a StatefulSet
func MakeStsSelectorLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	// Set a special selector to pick only the pods for this statefulset. It's
	// derived from the statefulset name as that stays constant and is unique in
	// a namespace.
	m[vmeta.SubclusterSelectorLabel] = sc.GetStatefulSetName(vdb)
	return m
}

// MakeDepSelectorLabels will create the selector labels for use within a Deployment
func MakeDepSelectorLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	appendProxyLabels(m, vdb, sc)
	return m
}

// MakeAnnotationsForSubclusterService returns a map of annotations
// for Subcluster sc's service under VerticaDB vdb.
func MakeAnnotationsForSubclusterService(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	annotations := MakeAnnotationsForObject(vdb)
	for k, v := range sc.ServiceAnnotations {
		annotations[k] = v
	}
	return annotations
}
