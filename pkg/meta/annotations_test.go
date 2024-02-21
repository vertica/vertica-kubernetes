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
		Ω(GetNMAResource(ann, corev1.ResourceLimitsMemory)).Should(Equal(DefaultNMAResources[corev1.ResourceLimitsMemory]))
		Ω(GetNMAResource(ann, corev1.ResourceRequestsMemory)).Should(Equal(DefaultNMAResources[corev1.ResourceRequestsMemory]))
		Ω(GetNMAResource(ann, corev1.ResourceLimitsCPU)).Should(Equal(DefaultNMAResources[corev1.ResourceLimitsCPU]))
		Ω(GetNMAResource(ann, corev1.ResourceRequestsCPU)).Should(Equal(DefaultNMAResources[corev1.ResourceRequestsCPU]))
	})

	It("should allow NMA sidecar resource to be overridden", func() {
		ann := makeResourceAnnotations(GenNMAResourcesAnnotationName)
		Ω(GetNMAResource(ann, corev1.ResourceLimitsMemory)).Should(Equal(resource.MustParse("800Mi")))
		Ω(GetNMAResource(ann, corev1.ResourceRequestsMemory)).Should(Equal(DefaultNMAResources[corev1.ResourceRequestsMemory]))
		Ω(GetNMAResource(ann, corev1.ResourceLimitsCPU)).Should(Equal(resource.Quantity{}))
		Ω(GetNMAResource(ann, corev1.ResourceRequestsCPU)).Should(Equal(resource.MustParse("4")))
	})

	It("should allow the NMA health probe to be overridden", func() {
		ann := map[string]string{
			GenNMAHealthProbeAnnotationName(NMAHealthProbeStartup, NMAHealthProbeTimeoutSeconds):   "33",
			GenNMAHealthProbeAnnotationName(NMAHealthProbeStartup, NMAHealthProbeFailureThreshold): "bad-filter",
			GenNMAHealthProbeAnnotationName(NMAHealthProbeStartup, NMAHealthProbeSuccessThreshold): "-5",
		}
		v, ok := GetNMAHealthProbeOverride(ann, NMAHealthProbeStartup, NMAHealthProbeTimeoutSeconds)
		Ω(ok).Should(BeTrue())
		Ω(v).Should(Equal(int32(33)))
		_, ok = GetNMAHealthProbeOverride(ann, NMAHealthProbeStartup, NMAHealthProbeFailureThreshold)
		Ω(ok).Should(BeFalse())
		_, ok = GetNMAHealthProbeOverride(ann, NMAHealthProbeStartup, NMAHealthProbePeriodSeconds)
		Ω(ok).Should(BeFalse())
		_, ok = GetNMAHealthProbeOverride(ann, NMAHealthProbeStartup, NMAHealthProbeSuccessThreshold)
		Ω(ok).Should(BeFalse())
	})

	It("should return the scrutinize pod ttl based on the annotations map", func() {
		ann := map[string]string{}
		Ω(GetScrutinizePodTTL(ann)).Should(Equal(ScrutinizePodTTLDefaultValue))

		ann = map[string]string{
			ScrutinizePodTTLAnnotation: "-1",
		}
		Ω(GetScrutinizePodTTL(ann)).Should(Equal(ScrutinizePodTTLDefaultValue))

		ann = map[string]string{
			ScrutinizePodTTLAnnotation: "not a number",
		}
		Ω(GetScrutinizePodTTL(ann)).Should(Equal(ScrutinizePodTTLDefaultValue))

		const ttlStr = "180"
		const ttl = 180
		ann = map[string]string{
			ScrutinizePodTTLAnnotation: ttlStr,
		}
		Ω(GetScrutinizePodTTL(ann)).Should(Equal(ttl))
	})

	It("should return the scrutinize pod restart policy based on the annotations map", func() {
		ann := map[string]string{}
		Ω(GetScrutinizePodRestartPolicy(ann)).Should(Equal(string(corev1.RestartPolicyNever)))

		ann = map[string]string{
			ScrutinizePodRestartPolicyAnnotation: "wrong-policy",
		}
		Ω(GetScrutinizePodRestartPolicy(ann)).Should(Equal(string(corev1.RestartPolicyNever)))

		ann = map[string]string{
			ScrutinizePodRestartPolicyAnnotation: string(corev1.RestartPolicyAlways),
		}
		Ω(GetScrutinizePodRestartPolicy(ann)).Should(Equal(string(corev1.RestartPolicyAlways)))
	})

	It("should return scrutinize main container image based on the annotations map", func() {
		ann := map[string]string{}
		Ω(GetScrutinizeMainContainerImage(ann)).Should(Equal(ScrutinizeMainContainerImageDefaultValue))

		ann = map[string]string{
			ScrutinizeMainContainerImageAnnotation: "",
		}
		Ω(GetScrutinizeMainContainerImage(ann)).Should(Equal(ScrutinizeMainContainerImageDefaultValue))

		const img = "busybox:latest"
		ann = map[string]string{
			ScrutinizeMainContainerImageAnnotation: img,
		}
		Ω(GetScrutinizeMainContainerImage(ann)).Should(Equal(img))
	})

	It("should return default scrutinize main container resources", func() {
		ann := map[string]string{}
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceLimitsMemory)).
			Should(Equal(DefaultScrutinizeMainContainerResources[corev1.ResourceLimitsMemory]))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceRequestsMemory)).
			Should(Equal(DefaultScrutinizeMainContainerResources[corev1.ResourceRequestsMemory]))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceLimitsCPU)).
			Should(Equal(DefaultScrutinizeMainContainerResources[corev1.ResourceLimitsCPU]))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceRequestsCPU)).
			Should(Equal(DefaultScrutinizeMainContainerResources[corev1.ResourceRequestsCPU]))
	})

	It("should allow scrutinize main container resources to be overridden", func() {
		ann := makeResourceAnnotations(GenScrutinizeMainContainerResourcesAnnotationName)
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceLimitsMemory)).
			Should(Equal(resource.MustParse("800Mi")))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceRequestsMemory)).
			Should(Equal(DefaultNMAResources[corev1.ResourceRequestsMemory]))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceLimitsCPU)).
			Should(Equal(resource.Quantity{}))
		Ω(GetScrutinizeMainContainerResource(ann, corev1.ResourceRequestsCPU)).
			Should(Equal(resource.MustParse("4")))
	})
})

func makeResourceAnnotations(fn func(resourceName corev1.ResourceName) string) map[string]string {
	return map[string]string{
		fn(corev1.ResourceLimitsMemory):   "800Mi",
		fn(corev1.ResourceRequestsMemory): "unparseable",
		fn(corev1.ResourceLimitsCPU):      "",
		fn(corev1.ResourceRequestsCPU):    "4",
	}
}
