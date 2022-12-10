/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package version

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
)

var _ = Describe("metadata", func() {
	It("parsing of version output should return expected annotations", func() {
		op := `Vertica Analytic Database v11.0.0-20210601
vertica(v11.0.0-20210601) built by @re-docker2 from master@da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7 on 'Tue Jun  1 05:04:35 2021' $BuildId$`
		ans := ParseVersionOutput(op)
		const NumAnnotations = 3
		Expect(len(ans)).Should(Equal(NumAnnotations))
		Expect(ans[vapi.VersionAnnotation]).Should(Equal("v11.0.0-20210601"))
		Expect(ans[vapi.BuildDateAnnotation]).Should(Equal("Tue Jun  1 05:04:35 2021"))
		Expect(ans[vapi.BuildRefAnnotation]).Should(Equal("da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7"))
	})

	It("should indicate no change if annotations stayed the same", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			vapi.BuildDateAnnotation: "Tue Jun 10",
			vapi.BuildRefAnnotation:  "abcd",
			vapi.VersionAnnotation:   "v11.0.0",
		}

		op := `Vertica Analytic Database v11.0.0
vertica(v11.0.0) built by @re-docker2 from master@abcd on 'Tue Jun 10' $BuildId$
`
		chg := vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeFalse())
	})

	It("should indicate a change if annotations changed", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			vapi.BuildDateAnnotation: "Tue Jun 10",
			vapi.BuildRefAnnotation:  "abcd",
			vapi.VersionAnnotation:   "v10.1.1-0",
		}

		op := `Vertica Analytic Database v11.0.0-1
vertica(v11.0.0-1) built by @re-docker2 from master@abcd on 'Tue Jun 10' $BuildId$
`
		chg := vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeTrue())

		vdb.ObjectMeta.Annotations = map[string]string{
			vapi.BuildDateAnnotation: "Tue Jun 10",
		}
		chg = vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeTrue())
	})

	It("should prevent downgrades", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			vapi.BuildDateAnnotation: "Tue Jun 10",
			vapi.BuildRefAnnotation:  "abcd",
			vapi.VersionAnnotation:   "v11.0.2",
		}

		// Pick a version from before v11.0.2
		newAnnotations := map[string]string{
			vapi.VersionAnnotation: "v11.0.1",
		}
		ok, failureReason := IsUpgradePathSupported(vdb, newAnnotations)
		Expect(ok).Should(BeFalse())
		Expect(failureReason).ShouldNot(Equal(""))
	})

	It("should be okay if vdb does not have version annotation", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{}
		newAnnotations := map[string]string{
			vapi.VersionAnnotation: "v11.0.1",
		}
		ok, failureReason := IsUpgradePathSupported(vdb, newAnnotations)
		Expect(ok).Should(BeTrue())
		Expect(failureReason).Should(Equal(""))
	})
})
