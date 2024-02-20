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

package vadmin

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("fetch_node_state_at", func() {
	ctx := context.Background()

	It("should call list_allnodes", func() {
		d, vdb, fpr := mockAdmintoolsDispatcher()
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 1)
		fpr.Results[atPod] = []cmds.CmdResult{
			{Stdout: " Node          | Host       | State | Version                 | DB \n" +
				"---------------+------------+-------+-------------------------+----\n" +
				" v_d_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | d \n" +
				" v_d_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | d \n" +
				" v_d_node0003 | 10.244.1.8 | UP    | vertica-11.0.0.20210309 | d \n" +
				"\n",
			},
		}
		state, res, err := d.FetchNodeState(ctx,
			fetchnodestate.WithInitiator(atPod, "10.244.1.6"),
		)
		Ω(err).Should(Succeed())
		Ω(res).Should(Equal(ctrl.Result{}))
		Ω("v_d_node0001").Should(BeKeyOf(state))
		Ω(state["v_d_node0001"]).Should(Equal("UP"))
		Ω("v_d_node0002").Should(BeKeyOf(state))
		Ω(state["v_d_node0002"]).Should(Equal("DOWN"))
		Ω("v_d_node0003").Should(BeKeyOf(state))
		Ω(state["v_d_node0003"]).Should(Equal("UP"))
	})

	It("should parse the list_allnodes output", func() {
		at, _, _ := mockAdmintoolsDispatcher()
		stateMap := at.parseClusterNodeStatus(
			" Node          | Host       | State | Version                 | DB \n" +
				"---------------+------------+-------+-------------------------+----\n" +
				" v_d_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db \n" +
				" v_d_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | db \n" +
				"\n",
		)
		n1, ok := stateMap["v_d_node0001"]
		Ω(ok).Should(BeTrue())
		Ω(n1).Should(Equal("UP"))
		n2, ok := stateMap["v_d_node0002"]
		Ω(ok).Should(BeTrue())
		Ω(n2).Should(Equal("DOWN"))
	})
})
