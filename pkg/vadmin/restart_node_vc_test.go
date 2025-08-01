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
	"fmt"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mock version of VStartNodes() that is invoked inside VClusterOps.StartNode()
func (m *MockVClusterOps) VStartNodes(options *vops.VStartNodesOptions) error {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return fmt.Errorf("failed to retrieve hosts")
	}

	// verify timeout
	if options.StatePollingTimeout != TestTimeout {
		return fmt.Errorf("failed to retrieve timeout")
	}

	if m.ReturnReIPNoClusterQuorum {
		return &vops.ReIPNoClusterQuorumError{Detail: "cluster quorum loss"}
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
		dispatcher.VDB.Annotations[vmeta.RestartTimeoutAnnotation] = "10"
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = "restart-node-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)

		ctrlRes, err := callStartNodes(ctx, dispatcher, nodeIPs)
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})

	It("should detect ReIPNoClusterQuorumError", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.DBName = TestDBName
		vdb.Spec.HTTPSNMATLS.Secret = TestNMATLSSecret
		vdb.Annotations[vmeta.RestartTimeoutAnnotation] = "10"
		setupAPIFunc := func(logr.Logger, string) (VClusterProvider, logr.Logger) {
			return &MockVClusterOps{ReturnReIPNoClusterQuorum: true}, logr.Logger{}
		}
		cacheManager := cache.MakeCacheManager(true)
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc, cacheManager)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		ctrlRes, err := callStartNodes(ctx, dispatcher, nodeIPs)
		Ω(err).Should(Succeed())
		Ω(ctrlRes.Requeue).Should(BeTrue())
	})
})

func callStartNodes(ctx context.Context, dispatcher *VClusterOps, nodeIPs []string) (ctrl.Result, error) {
	return dispatcher.RestartNode(ctx,
		restartnode.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
		restartnode.WithHost("vnode1", nodeIPs[1]),
	)
}
