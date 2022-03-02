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

package controllers

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
	VDBInstanceLabel          = "app.kubernetes.io/instance"
	OperatorVersionLabel      = "app.kubernetes.io/version"
	OperatorName              = "verticadb-operator" // The name of the operator

	CurOperatorVersion = "1.3.1" // The version number of the operator
	OperatorVersion100 = "1.0.0"
	OperatorVersion110 = "1.1.0"
	OperatorVersion120 = "1.2.0"
	OperatorVersion130 = "1.3.0"
	OperatorVersion131 = CurOperatorVersion
)

// makeSubclusterLabels returns the labels added for the subcluster
func makeSubclusterLabels(sc *vapi.Subcluster) map[string]string {
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

// makeOperatorLabels returns the labels that all objects created by this operator will have
func makeOperatorLabels(vdb *vapi.VerticaDB) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": OperatorName,
		"app.kubernetes.io/name":       "vertica",
		VDBInstanceLabel:               vdb.Name,
		"app.kubernetes.io/component":  "database",
		"vertica.com/database":         vdb.Spec.DBName,
	}
}

// makeCommonLabels returns the labels that are common to all objects.
func makeCommonLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	labels := makeOperatorLabels(vdb)
	// Apply a label to indicate a version of the operator that created the
	// object.  This is separate from makeOperatorLabels as we don't want to
	// include that in any sort of label selector.
	labels[OperatorVersionLabel] = CurOperatorVersion

	// Remaining labels are for objects that are subcluster specific
	if sc == nil {
		return labels
	}

	for k, v := range makeSubclusterLabels(sc) {
		labels[k] = v
	}

	return labels
}

// makeLabelsForObjects constructs the labels for a new k8s object
func makeLabelsForObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	labels := makeCommonLabels(vdb, sc)

	// Add any custom labels that were in the spec.
	for k, v := range vdb.Spec.Labels {
		labels[k] = v
	}

	return labels
}

// makeLabelsForSvcObject will create the set of labels for use with service objects
func makeLabelsForSvcObject(vdb *vapi.VerticaDB, sc *vapi.Subcluster, svcType string) map[string]string {
	labels := makeLabelsForObject(vdb, sc)
	labels[SvcTypeLabel] = svcType
	return labels
}

// makeAnnotationsForObjects builds the list of annotations that are to be
// included on new objects.
func makeAnnotationsForObject(vdb *vapi.VerticaDB) map[string]string {
	annotations := make(map[string]string, len(vdb.Spec.Annotations))
	for k, v := range vdb.Spec.Annotations {
		annotations[k] = v
	}
	return annotations
}

// makeSvcSelectorLabels returns the labels that are used for selectors in service objects.
func makeBaseSvcSelectorLabels(vdb *vapi.VerticaDB) map[string]string {
	// We intentionally don't use the common labels because that includes things
	// specific to the operator version.  To allow the selector to work with
	// pods created from an older operator, we need to be more selective in the
	// labels we choose.
	return map[string]string{
		VDBInstanceLabel: vdb.Name,
	}
}

// makeSvcSelectorLabelsForServiceNameRouting will create the labels for when we
// want a service object to pick the pods based on the service name.  This
// allows us to combine multiple subcluster under a single service object.
func makeSvcSelectorLabelsForServiceNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := makeBaseSvcSelectorLabels(vdb)
	m[SubclusterSvcNameLabel] = sc.GetServiceName()
	return m
}

// makeSvcSelectorLabelsForSubclusterNameRouting will create the labels for when
// we want a service object to pick the pods based on the subcluster name.
func makeSvcSelectorLabelsForSubclusterNameRouting(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	m := makeBaseSvcSelectorLabels(vdb)
	// Routing is done solely with the subcluster name.
	m[SubclusterNameLabel] = sc.Name
	return m
}
