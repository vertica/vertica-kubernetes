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

package nodeinfo

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("nodestatevsql", func() {
	It("should parse read-only and sanbox states from node query", func() {
		ninf, err := parseNodeState("v_db_node0001|UP|123456|t|sb1\n")
		Expect(err).Should(Succeed())
		Expect(ninf.ReadOnly).Should(BeTrue())
		Expect(ninf.SubclusterOid).Should(Equal("123456"))
		Expect(ninf.SandboxName).Should(Equal("sb1"))

		ninf, err = parseNodeState("v_db_node0001|UP|7890123|f|sb2\n")
		Expect(err).Should(Succeed())
		Expect(ninf.ReadOnly).Should(BeFalse())
		Expect(ninf.SubclusterOid).Should(Equal("7890123"))
		Expect(ninf.SandboxName).Should(Equal("sb2"))

		ninf, err = parseNodeState("v_db_node0001|UP|456789\n")
		Expect(err).Should(Succeed())
		Expect(ninf.ReadOnly).Should(BeFalse())
		Expect(ninf.SubclusterOid).Should(Equal("456789"))

		_, err = parseNodeState("")
		Expect(err).Should(Succeed())

		_, err = parseNodeState("v_db_node0001|UP|123|t|garbage")
		Expect(err).Should(Succeed())
	})
})
