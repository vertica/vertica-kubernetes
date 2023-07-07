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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
)

// mock version of VFetchNodeState() that is invoked inside VClusterOps.FetchNodeState()
func (m *MockVClusterOps) VFetchNodeState(options *vops.VFetchNodeStateOptions) ([]vops.NodeInfo, error) {
	// TODO: call verify common options, but exclude the DB name

	// verify basic options
	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return nil, fmt.Errorf("failed to retrieve hosts")
	}

	// TODO: build a node info list
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

	It("should call vcluster-ops library with fetch_node_state task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		Ω(dispatcher.FetchNodeState(ctx, fetchnodestate.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP))).Should(Succeed())
	})
})
