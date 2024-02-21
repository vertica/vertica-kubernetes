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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// The default values to use for the NMA sidecar resources.  These can be
	// overridden with annotations (see NMAResourcesPrefixAnnotation).
	//
	// We intentionally omit limits. The workload in the NMA container is
	// burstable. The NMA itself is fairly stable but when it calls out to
	// processes like the catalog editor, it can bump the memory usage if the
	// catalog is large.
	DefaultNMAResources = map[corev1.ResourceName]resource.Quantity{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("250Mi"),
	}

	// The minimum memory limit for the NMA. This is picked because some of the
	// programs that the NMA calls out (bootstrap-catalog, catalog editor, etc.)
	// do not run if there is less than 1Gi of total memory.
	MinNMAMemoryLimit = resource.MustParse("1Gi")

	DefaultScrutinizeMainContainerResources = map[corev1.ResourceName]resource.Quantity{
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceRequestsMemory: resource.MustParse("250Mi"),
	}
)
