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

package v1beta1

import (
	"fmt"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("version", func() {
	It("parsing of version output should return expected annotations", func() {
		op := `Vertica Analytic Database v11.0.0-20210601
vertica(v11.0.0-20210601) built by @re-docker2 from master@da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7 on 'Tue Jun  1 05:04:35 2021' $BuildId$`
		ans := ParseVersionOutput(op)
		const NumAnnotations = 3
		Expect(len(ans)).Should(Equal(NumAnnotations))
		Expect(ans[VersionAnnotation]).Should(Equal("v11.0.0-20210601"))
		Expect(ans[BuildDateAnnotation]).Should(Equal("Tue Jun  1 05:04:35 2021"))
		Expect(ans[BuildRefAnnotation]).Should(Equal("da8f0e93f1ee720d8e4f8e1366a26c0d9dd7f9e7"))
	})

	It("should indicate no change if annotations stayed the same", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			BuildDateAnnotation: "Tue Jun 10",
			BuildRefAnnotation:  "abcd",
			VersionAnnotation:   "v11.0.0",
		}

		op := `Vertica Analytic Database v11.0.0
vertica(v11.0.0) built by @re-docker2 from master@abcd on 'Tue Jun 10' $BuildId$
`
		chg := vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeFalse())
	})

	It("should indicate a change if annotations changed", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			BuildDateAnnotation: "Tue Jun 10",
			BuildRefAnnotation:  "abcd",
			VersionAnnotation:   "v10.1.1-0",
		}

		op := `Vertica Analytic Database v11.0.0-1
vertica(v11.0.0-1) built by @re-docker2 from master@abcd on 'Tue Jun 10' $BuildId$
`
		chg := vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeTrue())

		vdb.ObjectMeta.Annotations = map[string]string{
			BuildDateAnnotation: "Tue Jun 10",
		}
		chg = vdb.MergeAnnotations(ParseVersionOutput(op))
		Expect(chg).Should(BeTrue())
	})

	It("should prevent downgrades", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{
			BuildDateAnnotation: "Tue Jun 10",
			BuildRefAnnotation:  "abcd",
			VersionAnnotation:   "v11.0.2",
		}

		// Pick a version from before v11.0.2
		newAnnotations := map[string]string{
			VersionAnnotation: "v11.0.1",
		}
		ok, failureReason := vdb.IsUpgradePathSupported(newAnnotations)
		Expect(ok).Should(BeFalse())
		Expect(failureReason).ShouldNot(Equal(""))
	})

	It("should be okay if vdb does not have version annotation", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations = map[string]string{}
		newAnnotations := map[string]string{
			VersionAnnotation: "v11.0.1",
		}
		ok, failureReason := vdb.IsUpgradePathSupported(newAnnotations)
		Expect(ok).Should(BeTrue())
		Expect(failureReason).Should(Equal(""))
	})

	It("should report unsupported for any version older than the min", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations[VersionAnnotation] = "v10.0.0"

		vinf, ok := vdb.MakeVersionInfo()
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported(MinimumVersion)).Should(BeTrue())
		Expect(vinf.IsSupported(MinimumVersion)).Should(BeFalse())
	})

	It("should report supported for any version newer than the min", func() {
		major, _ := strconv.Atoi(MinimumVersion[1:3])
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations[VersionAnnotation] = fmt.Sprintf("v%d%s", major+1, MinimumVersion[3:])
		vinf, ok := vdb.MakeVersionInfo()
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported(MinimumVersion)).Should(BeFalse())
		Expect(vinf.IsSupported(MinimumVersion)).Should(BeTrue())

		vdb.ObjectMeta.Annotations[VersionAnnotation] = fmt.Sprintf("%s-1", MinimumVersion)
		vinf, ok = vdb.MakeVersionInfo()
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported(MinimumVersion)).Should(BeFalse())
		Expect(vinf.IsSupported(MinimumVersion)).Should(BeTrue())
	})

	It("should fail to create Info if version not in vdb", func() {
		vdb := MakeVDB()
		_, ok := vdb.MakeVersionInfo()
		Expect(ok).Should(BeFalse())
	})

	It("should support a hot fix version of the minimum release", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.Annotations[VersionAnnotation] = MinimumVersion + "-8"
		vinf, ok := vdb.MakeVersionInfo()
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported(MinimumVersion)).Should(BeFalse())
		Expect(vinf.IsSupported(MinimumVersion)).Should(BeTrue())
	})
})
