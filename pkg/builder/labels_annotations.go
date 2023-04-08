/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"strconv"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

const (
	SvcTypeLabel              = "vertica.com/svc-type"
	SubclusterNameLabel       = "vertica.com/subcluster-name"
	SubclusterLegacyNameLabel = "vertica.com/subcluster"
	SubclusterTypeLabel       = "vertica.com/subcluster-type"
	SubclusterSvcNameLabel    = "vertica.com/subcluster-svc"
	SubclusterTransientLabel  = "vertica.com/subcluster-transient"

	// ClientRoutingLabel is a label that must exist on the pod in
	// order for Service objects to route to the pod.  This label isn't part of
	// the template in the StatefulSet.  This label is added after the pod is
	// scheduled.  There are a couple of uses for it:
	// - after an add node, we only add the labels once the node has at least
	// one shard subscription.  This saves routing to a pod that cannot fulfill
	// a query request.
	// - before we remove a node.  It allows us to drain out pods that are going
	// to be removed by a pending node removal.
	ClientRoutingLabel = "vertica.com/client-routing"
	ClientRoutingVal   = "true"

	VDBInstanceLabel     = "app.kubernetes.io/instance"
	OperatorVersionLabel = "app.kubernetes.io/version"
	ManagedByLabel       = "app.kubernetes.io/managed-by"
	OperatorName         = "verticadb-operator" // The name of the operator

	CurOperatorVersion = "1.10.2" // The version number of the operator
	// If any of the operator versions are used in the code, add a const here.
	// But it isn't necessary to create a const for each version.
	OperatorVersion100 = "1.0.0"
	OperatorVersion110 = "1.1.0"
	OperatorVersion120 = "1.2.0"
	OperatorVersion130 = "1.3.0"

	// Annotations that we set in each of the pod.  These are set by the
	// AnnotateAndLabelPodReconciler.  They are available in the pod with the
	// downwardAPI so they can be picked up by the Vertica data collector (DC).
	KubernetesVersionAnnotation   = "kubernetes.io/version"   // Version of the k8s server
	KubernetesGitCommitAnnotation = "kubernetes.io/gitcommit" // Git commit of the k8s server
	KubernetesBuildDateAnnotation = "kubernetes.io/buildDate" // Build date of the k8s server
)

// MakeSubclusterLabels returns the labels added for the subcluster
func MakeSubclusterLabels(sc *vapi.Subcluster) map[string]string {
	m := map[string]string{
		SubclusterNameLabel:      sc.Name,
		SubclusterTypeLabel:      sc.GetType(),
		SubclusterTransientLabel: strconv.FormatBool(sc.IsTransient),
	}
	// Transient subclusters never have the service name label set.  At various
	// parts of the upgrade, it will accept traffic from all of the subclusters.
	if !sc.IsTransient {
		m[SubclusterSvcNameLabel] = sc.GetServiceName()
	}
	return m
}

// MakeOperatorLabels returns the labels that all objects created by this operator will have
func MakeOperatorLabels(vdb *vapi.VerticaDB) map[string]string {
	return map[string]string{
		ManagedByLabel:                OperatorName,
		"app.kubernetes.io/name":      "vertica",
		VDBInstanceLabel:              vdb.Name,
		"app.kubernetes.io/component": "database",
		"vertica.com/database":        vdb.Spec.DBName,
	}
}

// MakeCommonLabels returns the labels that are common to all objects.
func MakeCommonLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster, forPod bool) map[string]string {
	labels := MakeOperatorLabels(vdb)
	if !forPod {
		// Apply a label to indicate a version of the operator that created the
		// object.  This is separate from MakeOperatorLabels as we don't want to
		// set this for pods in the template.  We set the operator version in
		// the pods as part of a reconciler so that we don't have to reschedule
		// the pods.
		labels[OperatorVersionLabel] = CurOperatorVersion
	}

	// Remaining labels are for objects that are subcluster specific
	if sc == nil {
		return labels
	}

	for k, v := range MakeSubclusterLabels(sc) {
		labels[k] = v
	}

	return labels
}

// MakeLabelsForObjects constructs the labels for a new k8s object
func makeLabelsForObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, forPod bool) map[string]string {
	labels := MakeCommonLabels(vdb, sc, forPod)

	// Add any custom labels that were in the spec.
	for k, v := range vdb.Spec.Labels {
		labels[k] = v
	}

	return labels
}

// MakeLabelsForPodObject constructs the labels that are common for all pods
func MakeLabelsForPodObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	return makeLabelsForObject(vdb, sc, true)
}

// MakeLabelsForStsObject constructs the labels that are common for all statefulsets.
func MakeLabelsForStsObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	return makeLabelsForObject(vdb, sc, false)
}

// MakeLabelsForSvcObject will create the set of labels for use with service objects
func MakeLabelsForSvcObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, svcType string) map[string]string {
	labels := makeLabelsForObject(vdb, sc, false)
	labels[SvcTypeLabel] = svcType
	return labels
}

// MakeAnnotationsForObjects builds the list of annotations that are to be
// included on new objects.
func MakeAnnotationsForObject(vdb *vapi.VerticaDB) map[string]string {
	annotations := make(map[string]string, len(vdb.Spec.Annotations))
	for k, v := range vdb.Spec.Annotations {
		annotations[k] = v
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
		VDBInstanceLabel: vdb.Name,
	}
}

// MakeSvcSelectorLabelsForServiceNameRouting will create the labels for when we
// want a service object to pick the pods based on the service name.  This
// allows us to combine multiple subcluster under a single service object.
func MakeSvcSelectorLabelsForServiceNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	m[SubclusterSvcNameLabel] = sc.GetServiceName()
	// Only route to nodes that have verified they own at least one shard and
	// aren't pending delete
	m[ClientRoutingLabel] = ClientRoutingVal
	return m
}

// MakeSvcSelectorLabelsForSubclusterNameRouting will create the labels for when
// we want a service object to pick the pods based on the subcluster name.
func MakeSvcSelectorLabelsForSubclusterNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	// Routing is done using the subcluster name rather than the service name.
	m[SubclusterNameLabel] = sc.Name
	m[ClientRoutingLabel] = ClientRoutingVal

	return m
}

// MakeStsSelectorLabels will create the selector labels for use within a StatefulSet
func MakeStsSelectorLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := MakeBaseSvcSelectorLabels(vdb)
	m[SubclusterNameLabel] = sc.Name
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
