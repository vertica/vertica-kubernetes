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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("restart_node_at", func() {
	ctx := context.Background()

	It("should call admintools -t restart_node", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		res, err := dispatcher.RestartNode(ctx,
			restartnode.WithInitiator(nm, "10.8.1.1"),
			restartnode.WithHost("v_db_node0001", "10.8.1.10"),
			restartnode.WithHost("v_db_node0002", "10.8.1.11"),
		)
		Ω(err).Should(Succeed())
		Ω(res).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("-t restart_node")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElements(
			"--hosts=v_db_node0001,v_db_node0002",
			"--new-host-ips=10.8.1.10,10.8.1.11",
		))
	})
})
