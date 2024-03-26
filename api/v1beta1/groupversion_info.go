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

// Package v1beta1 contains API Schema definitions for the  v1beta1 API group
// +kubebuilder:object:generate=true
// +groupName=vertica.com
package v1beta1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

const (
	Group   = "vertica.com"
	Version = "v1beta1"

	VerticaDBKind          = "VerticaDB"
	VerticaAutoscalerKind  = "VerticaAutoscaler"
	EventTriggerKind       = "EventTrigger"
	RestorePointsQueryKind = "VerticaRestorePointsQuery"
	VerticaScrutinizeKind  = "VerticaScrutinize"
	VerticaReplicatorKind  = "VerticaReplicator"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: Group, Version: Version}

	// SchemeBuilder is used to add go types to the GroupVersionKind scheme
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group-version to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme

	// All supported group/kind by this operator
	GkVDB  = schema.GroupKind{Group: Group, Kind: VerticaDBKind}
	GkVAS  = schema.GroupKind{Group: Group, Kind: VerticaAutoscalerKind}
	GkET   = schema.GroupKind{Group: Group, Kind: EventTriggerKind}
	GkVRPQ = schema.GroupKind{Group: Group, Kind: RestorePointsQueryKind}
	GkVSCR = schema.GroupKind{Group: Group, Kind: VerticaScrutinizeKind}
	GkVR   = schema.GroupKind{Group: Group, Kind: VerticaReplicatorKind}
)
