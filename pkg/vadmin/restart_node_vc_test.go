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
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mock version of VStartDatabase() that is invoked inside VClusterOps.StartDB()
func (m *MockVClusterOps) VRestartNodes(options *vops.VRestartNodesOptions) error {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify eon options
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	return nil
}

var _ = Describe("restart_node_vc", func() {
	ctx := context.Background()

	var nodeIPs []string
	for i := 0; i < 3; i++ {
		nodeIP := fmt.Sprintf("10.10.10.1%d", i)
		nodeIPs = append(nodeIPs, nodeIP)
	}

	It("should call vcluster-ops library with restart_node task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.HTTPServerTLSSecret = "restart-node-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)

		ctrlRes, err := dispatcher.RestartNode(ctx,
			restartnode.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
			restartnode.WithHost("vnode1", nodeIPs[1]),
		)
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
