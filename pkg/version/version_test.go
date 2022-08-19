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
	"fmt"
	"strconv"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecsWithDefaultAndCustomReporters(t,
		"version Suite",
		[]Reporter{printer.NewlineReporter{}})
}

var _ = Describe("version", func() {
	It("should report unsupported for any version older than the min", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v10.0.0"

		vinf, ok := MakeInfoFromVdb(vdb)
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported()).Should(BeTrue())
		Expect(vinf.IsSupported()).Should(BeFalse())
	})

	It("should report supported for any version newer than the min", func() {
		major, _ := strconv.Atoi(MinimumVersion[1:3])
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = fmt.Sprintf("v%d%s", major+1, MinimumVersion[3:])
		vinf, ok := MakeInfoFromVdb(vdb)
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported()).Should(BeFalse())
		Expect(vinf.IsSupported()).Should(BeTrue())

		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = fmt.Sprintf("%s-1", MinimumVersion)
		vinf, ok = MakeInfoFromVdb(vdb)
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported()).Should(BeFalse())
		Expect(vinf.IsSupported()).Should(BeTrue())
	})

	It("should fail to create Info if version not in vdb", func() {
		vdb := vapi.MakeVDB()
		_, ok := MakeInfoFromVdb(vdb)
		Expect(ok).Should(BeFalse())
	})

	It("should support a hot fix version of the minimum release", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = MinimumVersion + "-8"
		vinf, ok := MakeInfoFromVdb(vdb)
		Expect(ok).Should(BeTrue())
		Expect(vinf.IsUnsupported()).Should(BeFalse())
		Expect(vinf.IsSupported()).Should(BeTrue())
	})

	It("should block some version transitions", func() {
		cur, ok := MakeInfoFromStr("v11.0.1")
		Expect(ok).Should(BeTrue())
		ok, _ = cur.IsValidUpgradePath("v11.0.0")
		Expect(ok).Should(BeFalse())
		ok, _ = cur.IsValidUpgradePath("v10.1.1")
		Expect(ok).Should(BeFalse())
		ok, _ = cur.IsValidUpgradePath("v11.1.0")
		Expect(ok).Should(BeTrue())
		ok, _ = cur.IsValidUpgradePath("v11.0.2")
		Expect(ok).Should(BeTrue())
		ok, _ = cur.IsValidUpgradePath("v11.2.2")
		Expect(ok).Should(BeFalse()) // Fail because it skips v11.1.x

		cur, ok = MakeInfoFromStr("v15.1.1")
		Expect(ok).Should(BeTrue())
		ok, _ = cur.IsValidUpgradePath("v14.0.2")
		Expect(ok).Should(BeFalse())
		ok, _ = cur.IsValidUpgradePath("v16.1.3")
		Expect(ok).Should(BeTrue())
	})

	It("should return values for IsOlder", func() {
		cur, ok := MakeInfoFromStr("v12.0.1")
		Expect(ok).Should(BeTrue())
		ok = cur.IsOlder("v12.0.0")
		Expect(ok).Should(BeFalse())
		ok = cur.IsOlder("v12.0.1")
		Expect(ok).Should(BeFalse())
		ok = cur.IsOlder("v13.1.1")
		Expect(ok).Should(BeTrue())
	})
})
