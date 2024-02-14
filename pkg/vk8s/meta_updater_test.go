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

package vk8s

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("vk8s/meta_updater_test", func() {
	ctx := context.Background()

	It("should add labels/annotations to a VerticaDB object", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		chgs := MetaChanges{
			NewLabels: map[string]string{
				"label1": "value1",
			},
			NewAnnotations: map[string]string{
				"ann1": "value2",
			},
		}
		Ω(MetaUpdate(ctx, k8sClient, vdb.ExtractNamespacedName(), vdb, chgs)).Should(Equal(true))
		fetchVDB := vapi.VerticaDB{}
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), &fetchVDB)).Should(Succeed())
		Ω(fetchVDB.Labels["label1"]).Should(Equal("value1"))
		Ω(fetchVDB.Annotations["ann1"]).Should(Equal("value2"))
	})

	It("should add annotations to a Pod", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pod := &corev1.Pod{}
		chgs := MetaChanges{
			NewAnnotations: map[string]string{
				"podAnn1": "expected-value",
				"podAnn2": "another-value",
			},
		}
		Ω(MetaUpdate(ctx, k8sClient, pn, pod, chgs)).Should(Equal(true))
		fetchPod := &corev1.Pod{}
		Ω(k8sClient.Get(ctx, pn, fetchPod)).Should(Succeed())
		Ω(fetchPod.Annotations["podAnn1"]).Should(Equal("expected-value"))
		Ω(fetchPod.Annotations["podAnn2"]).Should(Equal("another-value"))

		chgs = MetaChanges{
			NewAnnotations: map[string]string{
				"podAnn1": "overwrite-value",
			},
			NewLabels: map[string]string{
				"label1": "label-val",
			},
		}
		Ω(MetaUpdate(ctx, k8sClient, pn, pod, chgs)).Should(Equal(true))
		Ω(k8sClient.Get(ctx, pn, fetchPod)).Should(Succeed())
		Ω(fetchPod.Annotations["podAnn1"]).Should(Equal("overwrite-value"))
		Ω(fetchPod.Labels["label1"]).Should(Equal("label-val"))
	})

	It("should be a no-op if object doesn't exist or labels already set", func() {
		vdb := vapi.MakeVDB()
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pod := &corev1.Pod{}
		chgs := MetaChanges{
			NewLabels: map[string]string{
				"l1": "l1-value",
				"l2": "l2-value",
			},
		}
		// Pod doesn't exist yet
		Ω(MetaUpdate(ctx, k8sClient, pn, pod, chgs)).Should(Equal(false))

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		Ω(MetaUpdate(ctx, k8sClient, pn, pod, chgs)).Should(Equal(true))
		fetchPod := &corev1.Pod{}
		Ω(k8sClient.Get(ctx, pn, fetchPod)).Should(Succeed())
		Ω(fetchPod.Labels["l1"]).Should(Equal("l1-value"))
		Ω(fetchPod.Labels["l2"]).Should(Equal("l2-value"))

		// Do the same update. Should return false because labels are already set
		Ω(MetaUpdate(ctx, k8sClient, pn, pod, chgs)).Should(Equal(false))
	})
})
