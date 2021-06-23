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

package names

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

func TestNames(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"names Suite",
		[]Reporter{printer.NewlineReporter{}})
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
})
