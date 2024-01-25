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
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestNames(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "annotations Suite")
}

var _ = Describe("annotations", func() {
	It("pause operator annotation should take different boolean values", func() {
		Ω(IsPauseAnnotationSet(nil)).Should(BeFalse())
		ann := map[string]string{}
		Ω(IsPauseAnnotationSet(ann)).Should(BeFalse())
		ann[PauseOperatorAnnotation] = "true"
		Ω(IsPauseAnnotationSet(ann)).Should(BeTrue())
		ann[PauseOperatorAnnotation] = "1"
		Ω(IsPauseAnnotationSet(ann)).Should(BeTrue())
		ann[PauseOperatorAnnotation] = "OFF"
		Ω(IsPauseAnnotationSet(ann)).Should(BeFalse())
		ann[PauseOperatorAnnotation] = "not a bool"
		Ω(IsPauseAnnotationSet(ann)).Should(BeFalse())
	})

	It("should treat vclusterOps annotation as a bool", func() {
		ann := map[string]string{VClusterOpsAnnotation: VClusterOpsAnnotationTrue}
		Ω(UseVClusterOps(ann)).Should(BeTrue())
	})

	It("should treat mountNMACerts annotation as a bool", func() {
		ann := map[string]string{MountNMACertsAnnotation: MountNMACertsAnnotationTrue}
		Ω(UseNMACertsMount(ann)).Should(BeTrue())
	})

	It("should return default NMA sidecar resources", func() {
		ann := map[string]string{}
		Ω(GetNMASidecarResource(ann, corev1.ResourceLimitsMemory)).Should(Equal(DefaultSidecarResource[corev1.ResourceLimitsMemory]))
		Ω(GetNMASidecarResource(ann, corev1.ResourceRequestsMemory)).Should(Equal(DefaultSidecarResource[corev1.ResourceRequestsMemory]))
		Ω(GetNMASidecarResource(ann, corev1.ResourceLimitsCPU)).Should(Equal(DefaultSidecarResource[corev1.ResourceLimitsCPU]))
		Ω(GetNMASidecarResource(ann, corev1.ResourceRequestsCPU)).Should(Equal(DefaultSidecarResource[corev1.ResourceRequestsCPU]))
	})

	It("should allow NMA sidecar resource to be overridden", func() {
		ann := map[string]string{
			GenNMASidecarResourceAnnotationName(corev1.ResourceLimitsMemory):   "800Mi",
			GenNMASidecarResourceAnnotationName(corev1.ResourceRequestsMemory): "unparseable",
			GenNMASidecarResourceAnnotationName(corev1.ResourceLimitsCPU):      "",
			GenNMASidecarResourceAnnotationName(corev1.ResourceRequestsCPU):    "4",
		}
		Ω(GetNMASidecarResource(ann, corev1.ResourceLimitsMemory)).Should(Equal(resource.MustParse("800Mi")))
		Ω(GetNMASidecarResource(ann, corev1.ResourceRequestsMemory)).Should(Equal(DefaultSidecarResource[corev1.ResourceRequestsMemory]))
		Ω(GetNMASidecarResource(ann, corev1.ResourceLimitsCPU)).Should(Equal(resource.Quantity{}))
		Ω(GetNMASidecarResource(ann, corev1.ResourceRequestsCPU)).Should(Equal(resource.MustParse("4")))
	})
})
