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

package vadmin

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	ctrl "sigs.k8s.io/controller-runtime"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
)

var _ = Describe("re_ip_at", func() {
	ctx := context.Background()

	It("should parse admintools.conf correctly in parseNodesFromAdmintoolsConf", func() {
		ips := parseNodesFromAdmintoolConf(
			"node0001 = 10.244.1.95,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0002 = 10.244.1.96,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0003 = 10.244.1.97,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0blah = no-ip,/data,/data\n" +
				"node0000 =badly formed\n",
		)
		Expect(ips["node0001"]).Should(Equal("10.244.1.95"))
		Expect(ips["node0002"]).Should(Equal("10.244.1.96"))
		Expect(ips["node0003"]).Should(Equal("10.244.1.97"))
		_, ok := ips["node0004"] // Will not find
		Expect(ok).Should(BeFalse())
		_, ok = ips["node0000"] // Will not find since it was badly formed
		Expect(ok).Should(BeFalse())
	})

	It("should use --force option in reip if on version that supports it", func() {
		at, vdb, _ := mockAdmintoolsDispatcher()
		vdb.Annotations[vapi.VersionAnnotation] = vapi.MinimumVersion
		Expect(at.genReIPCommand()).ShouldNot(ContainElement("--force"))
		vdb.Annotations[vapi.VersionAnnotation] = vapi.ReIPAllowedWithUpNodesVersion
		Expect(at.genReIPCommand()).Should(ContainElement("--force"))
	})

	It("should detect that map file has no IPs that are changing", func() {
		at, vdb, fpr := mockAdmintoolsDispatcher()

		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: "node0001 = 10.10.10.10"},
			},
		}
		Ω(at.ReIP(ctx,
			reip.WithInitiator(atPod, "10.10.10.10"),
			reip.WithHost("v_db_node0001", "node0001", "10.10.10.10"))).Should(Equal(ctrl.Result{}))
	})

	It("should only generate a map file for installed pods", func() {
		at, vdb, fpr := mockAdmintoolsDispatcher()

		const oldIP = "10.10.2.1"
		const newIP = "10.10.2.2"
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: fmt.Sprintf("node0001 = %s,/data,/data", oldIP)},
			},
		}
		Ω(at.ReIP(ctx,
			reip.WithInitiator(atPod, "10.10.10.10"),
			reip.WithHost("v_db_node0001", "node0001", newIP))).Should(Equal(ctrl.Result{}))
		c := fpr.FindCommands(fmt.Sprintf("%s %s", oldIP, newIP))
		Ω(len(c)).Should(Equal(1))
	})

	It("should fail if you supply no hosts", func() {
		at, vdb, _ := mockAdmintoolsDispatcher()
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		_, err := at.ReIP(ctx,
			reip.WithInitiator(atPod, "10.10.10.11"))
		Ω(err).ShouldNot(Succeed())

	})
})
