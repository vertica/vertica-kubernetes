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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mock version of VFetchNodeState() that is invoked inside VClusterOps.FetchNodeState()
func (m *MockVClusterOps) VFetchNodeState(options *vops.VFetchNodeStateOptions) ([]vops.NodeInfo, error) {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}

	// verify input hosts
	if len(options.RawHosts) == 0 {
		return nil, fmt.Errorf("failed to retrieve hosts")
	}

	// build a node info list
	dbName := TestDBName
	var nodeInfoList []vops.NodeInfo
	for i := 1; i <= 3; i++ {
		nodeName := fmt.Sprintf("v_%s_node000%d", dbName, i)
		nodeInfo := vops.NodeInfo{Name: nodeName, State: "UP"}
		nodeInfoList = append(nodeInfoList, nodeInfo)
	}
	return nodeInfoList, nil
}

var _ = Describe("fetch_node_state_vc", func() {
	ctx := context.Background()

	expectedResults := make(map[string]string)
	var nodeIPs []string
	for i := 1; i <= 3; i++ {
		nodeName := fmt.Sprintf("v_%s_node000%d", TestDBName, i)
		nodeIP := fmt.Sprintf("192.168.1.%d", i)
		nodeIPs = append(nodeIPs, nodeIP)
		expectedResults[nodeName] = "UP"
	}

	It("should call vcluster-ops library with fetch_node_state task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.NMATLSSecret = TestNMATLSSecret
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		dispatcher.VDB.Spec.DBName = TestDBName
		actualResults, ctrlRes, err := dispatcher.FetchNodeState(ctx,
			fetchnodestate.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
		)
		Ω(err).ShouldNot(HaveOccurred())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
		Ω(actualResults).Should(Equal(expectedResults))
	})
})
