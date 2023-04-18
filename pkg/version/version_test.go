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

package version

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "version Suite")
}

var _ = Describe("version", func() {
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
