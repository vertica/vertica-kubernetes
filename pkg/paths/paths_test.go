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

package paths

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"paths Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = Describe("paths", func() {
	const FakeUID = "abcdef"

	It("should include UID in path if IncludeUIDInPath is set", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Spec.Communal.IncludeUIDInPath = true
		Expect(GetCommunalPath(vdb)).Should(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should not include UID in path if IncludeUIDInPath is not set", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Spec.Communal.IncludeUIDInPath = false
		Expect(GetCommunalPath(vdb)).ShouldNot(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})
})
