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

package names

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
)

func TestNames(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "names Suite")
}

var _ = Describe("k8s/names", func() {
	It("pod name should include sts index", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Name = "name-test"
		vdb.ObjectMeta.Namespace = "my-ns"
		vdb.Spec.Subclusters[0].Name = "my-sc"
		Ω(GenPodName(vdb, &vdb.Spec.Subclusters[0], 9)).Should(Equal(
			types.NamespacedName{Namespace: "my-ns", Name: "name-test-my-sc-9"},
		))
	})

	It("pod dns name should be correct", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Name = "name-test"
		vdb.ObjectMeta.Namespace = "my-ns"
		vdb.Spec.Subclusters[0].Name = "my-sc"
		Ω(GenPodDNSName(vdb, &vdb.Spec.Subclusters[0], 9)).Should(Equal("name-test-my-sc-9.name-test.my-ns.svc.cluster.local"))
	})

	It("subcluster and external service generated names should not contain `_`", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Name = "v"
		vdb.ObjectMeta.Namespace = "v-ns"
		vdb.Spec.Subclusters[0].Name = "my_sc"
		Ω(GenStsName(vdb, &vdb.Spec.Subclusters[0])).Should(Equal(
			types.NamespacedName{Namespace: "v-ns", Name: "v-my-sc"},
		))
		Ω(GenExtSvcName(vdb, &vdb.Spec.Subclusters[0])).Should(Equal(
			types.NamespacedName{Namespace: "v-ns", Name: "v-my-sc"},
		))
	})
})
