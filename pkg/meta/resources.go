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
	// The default values to use for the NMA sidecar resource.  These can be
	// overridden with annotations (see NMASidecarResourcePrefix).
	DefaultSidecarResource = map[corev1.ResourceName]resource.Quantity{
		corev1.ResourceLimitsCPU:      resource.MustParse("1"),
		corev1.ResourceRequestsCPU:    resource.MustParse("1"),
		corev1.ResourceLimitsMemory:   resource.MustParse("250Mi"),
		corev1.ResourceRequestsMemory: resource.MustParse("250Mi"),
	}
)
