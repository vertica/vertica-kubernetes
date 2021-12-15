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

package v1beta1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("verticadb_types", func() {
	const FakeUID = "abcdef"

	It("should include UID in path if IncludeUIDInPath is set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Spec.Communal.IncludeUIDInPath = true
		Expect(vdb.GetCommunalPath()).Should(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should not include UID in path if IncludeUIDInPath is not set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Spec.Communal.IncludeUIDInPath = false
		Expect(vdb.GetCommunalPath()).ShouldNot(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should generate map of standbys", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1", IsPrimary: true},
			{Name: "sc1-standby", IsPrimary: false, IsStandby: true, StandbyParent: "sc1"},
			{Name: "sc2", IsPrimary: false},
			{Name: "sc3", IsPrimary: true},
			{Name: "sc3-standby", IsPrimary: false, IsStandby: true, StandbyParent: "sc3"},
		}
		m := vdb.GenSubclusterStandbyMap()
		Expect(m["sc1"]).Should(Equal("sc1-standby"))
		Expect(m["sc3"]).Should(Equal("sc3-standby"))
		Expect(m["sc2"]).Should(Equal(""))
	})
})
