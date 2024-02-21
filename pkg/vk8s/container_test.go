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

package vk8s

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("vk8s/container_test", func() {
	ctx := context.Background()

	It("should find the server container in the spec", func() {
		vdb := vapi.MakeVDBForHTTP("v-nma-tls-abcde")
		vdb.Spec.Image = vapi.NMAInSideCarDeploymentMinVersion
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Annotations[vmeta.VersionAnnotation] = vapi.NMAInSideCarDeploymentMinVersion
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pod := corev1.Pod{}
		Ω(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0), &pod)).Should(Succeed())
		Ω(GetServerContainer(pod.Spec.Containers)).ShouldNot(BeNil())
		Ω(GetServerImage(pod.Spec.Containers)).Should(Equal(vapi.NMAInSideCarDeploymentMinVersion))
		Ω(GetNMAContainer(pod.Spec.Containers)).ShouldNot(BeNil())

		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{
				Name:  names.NMAContainer,
				Ready: true,
			},
		}
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		Ω(GetNMAContainerStatus(pod.Status.ContainerStatuses)).ShouldNot(BeNil())
		Ω(IsNMAContainerReady(&pod.Status)).Should(BeTrue())
	})

	It("should find scrutinize init container status", func() {
		cntStatuses := []corev1.ContainerStatus{}
		Ω(GetScrutinizeInitContainerStatus(cntStatuses)).Should(BeNil())
		cntStatuses = append(cntStatuses, corev1.ContainerStatus{
			Name: names.ScrutinizeInitContainer,
		})
		stat := GetScrutinizeInitContainerStatus(cntStatuses)
		Ω(stat).ShouldNot(BeNil())
		Ω(stat.Name).Should(Equal(names.ScrutinizeInitContainer))
	})
})
