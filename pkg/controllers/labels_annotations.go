/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

import vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"

const (
	SvcTypeLabel    = "vertica.com/svc-type"
	SubclusterLabel = "vertica.com/subcluster"
	// The name of the operator
	OperatorName = "verticadb-operator"
	// The version number of the operator
	OperatorVersion = "1.3.0"
)

// makeSubclusterLabels returns the labels added for the subcluster
func makeSubclusterLabels(sc *vapi.Subcluster) map[string]string {
	return map[string]string{
		SubclusterLabel: sc.Name,
	}
}

// makeOperatorLabels returns the labels that all objects created by this operator will have
func makeOperatorLabels(vdb *vapi.VerticaDB) map[string]string {
	return map[string]string{
		"app.kubernetes.io/managed-by": OperatorName,
		"app.kubernetes.io/name":       "vertica",
		"app.kubernetes.io/instance":   vdb.Name,
		"app.kubernetes.io/version":    OperatorVersion,
		"app.kubernetes.io/component":  "database",
		"vertica.com/database":         vdb.Spec.DBName,
	}
}

// makeCommonLabels returns the labels that are common to all objects.
func makeCommonLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	labels := makeOperatorLabels(vdb)

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
func makeSvcSelectorLabels(vdb *vapi.VerticaDB, sc *vapi.Subcluster) map[string]string {
	// The selector will simply use the common labels for all objects.
	return makeCommonLabels(vdb, sc)
}
