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

package catalog

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("nodedetailsvsql", func() {
	It("should parse read-only and sandbox states from node query", func() {
		nodeDetails := &NodeDetails{}
		Expect(nodeDetails.parseNodeState("v_db_node0001|UP|123456|t|sb1\n")).Should(Succeed())
		Expect(nodeDetails.ReadOnly).Should(BeTrue())
		Expect(nodeDetails.SubclusterOid).Should(Equal("123456"))
		Expect(nodeDetails.SandboxName).Should(Equal("sb1"))

		nodeDetails = &NodeDetails{}
		Expect(nodeDetails.parseNodeState("v_db_node0001|UP|7890123|f|sb2\n")).Should(Succeed())
		Expect(nodeDetails.ReadOnly).Should(BeFalse())
		Expect(nodeDetails.SubclusterOid).Should(Equal("7890123"))
		Expect(nodeDetails.SandboxName).Should(Equal("sb2"))

		nodeDetails = &NodeDetails{}
		Expect(nodeDetails.parseNodeState("v_db_node0001|UP|456789\n")).Should(Succeed())
		Expect(nodeDetails.ReadOnly).Should(BeFalse())
		Expect(nodeDetails.SubclusterOid).Should(Equal("456789"))
		Expect(nodeDetails.SandboxName).Should(Equal(""))

		Expect(nodeDetails.parseNodeState("")).Should(Succeed())

		Expect(nodeDetails.parseNodeState("v_db_node0001|UP|123|t|garbage")).Should(Succeed())
	})

	It("should parse query output of shard subscriptions correctly", func() {
		nodeDetails := &NodeDetails{}
		Expect(nodeDetails.parseShardSubscriptions("3\n")).Should(Succeed())
		Expect(nodeDetails.ShardSubscriptions).Should(Equal(3))
		Expect(nodeDetails.parseDepotDetails("not-a-number\n")).ShouldNot(Succeed())
	})

	It("should parse query output of depot details correctly", func() {
		nodeDetails := &NodeDetails{}
		Expect(nodeDetails.parseDepotDetails("1248116736|60%\n")).Should(Succeed())
		Expect(nodeDetails.MaxDepotSize).Should(Equal(uint64(1248116736)))
		Expect(nodeDetails.DepotDiskPercentSize).Should(Equal("60%"))
		Expect(nodeDetails.parseDepotDetails("3248116736|\n")).Should(Succeed())
		Expect(nodeDetails.MaxDepotSize).Should(Equal(uint64(3248116736)))
		Expect(nodeDetails.DepotDiskPercentSize).Should(Equal(""))
		Expect(nodeDetails.parseDepotDetails("a|b|c")).ShouldNot(Succeed())
		Expect(nodeDetails.parseDepotDetails("not-a-number|blah")).ShouldNot(Succeed())
	})
})
