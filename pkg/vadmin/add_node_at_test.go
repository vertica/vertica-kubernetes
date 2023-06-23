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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
)

var _ = Describe("add_node_at", func() {
	ctx := context.Background()

	It("should call admintools -t db_add_node", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(dispatcher.AddNode(ctx,
			addnode.WithInitiator(nm, "10.9.1.1"),
			addnode.WithHost("v-main-1"),
			addnode.WithHost("v-main-2"),
		)).Should(Succeed())
		hist := fpr.FindCommands("-t db_add_node")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement("v-main-1,v-main-2"))
	})

	It("should return a special error when the license limit was reached", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results[nm] = []cmds.CmdResult{
			{
				Err: errors.New("admintools command failed"),
				Stdout: "There was an error adding the nodes to the database: DB client operation \"create nodes\" failed during `ddl`: " +
					"Severity: ROLLBACK, Message: Cannot create another node. The current license permits 3 node(s) and the database catalog " +
					"already contains 3 node(s), Sqlstate: V2001",
			},
		}
		err := dispatcher.AddNode(ctx,
			addnode.WithInitiator(nm, "10.9.1.2"),
			addnode.WithHost("v-main-0"),
		)
		Ω(err).ShouldNot(Succeed())
		_, ok := err.(*addnode.LicenseLimitError)
		Ω(ok).Should(BeTrue())
	})
})
