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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
)

var _ = Describe("verticadb_types", func() {
	const FakeUID = "abcdef"

	It("should include UID in path if IncludeUIDInPath is set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Annotations[vmeta.IncludeUIDInPathAnnotation] = "true"
		Expect(vdb.GetCommunalPath()).Should(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should not include UID in path if IncludeUIDInPath is not set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Annotations[vmeta.IncludeUIDInPathAnnotation] = "false"
		Expect(vdb.GetCommunalPath()).ShouldNot(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should require a transient subcluster", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1"},
			{Name: "sc2"},
		}
		// Transient is only required if specified
		Expect(vdb.RequiresTransientSubcluster()).Should(BeFalse())
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{"sc1"},
		}
		Expect(vdb.RequiresTransientSubcluster()).Should(BeFalse())
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Template: Subcluster{
				Name:      "the-transient-sc-name",
				Size:      1,
				IsPrimary: false,
			},
		}
		Expect(vdb.RequiresTransientSubcluster()).Should(BeTrue())
	})

	It("should return the first primary subcluster", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sec1", IsPrimary: false, Size: 1},
			{Name: "sec2", IsPrimary: false, Size: 1},
			{Name: "pri1", IsPrimary: true, Size: 1},
			{Name: "pri2", IsPrimary: true, Size: 1},
		}
		sc := vdb.GetFirstPrimarySubcluster()
		Ω(sc).ShouldNot(BeNil())
		Ω(sc.Name).Should(Equal("pri1"))
	})
})
