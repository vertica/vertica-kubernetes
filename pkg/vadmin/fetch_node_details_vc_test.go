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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodedetails"
)

// mock version of VFetchNodesDetails() that is invoked inside VClusterOps.FetchNodeDetails()
func (m *MockVClusterOps) VFetchNodesDetails(options *vops.VFetchNodesDetailsOptions) (vops.NodesDetails, error) {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}

	// verify input hosts
	if len(options.RawHosts) == 0 {
		return nil, fmt.Errorf("failed to retrieve hosts")
	}

	nodeDetails := vops.NodeDetails{}
	return vops.NodesDetails{nodeDetails}, nil
}

var _ = Describe("fetch_node_details_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with fetch_node_details task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		_, err := dispatcher.FetchNodeDetails(ctx,
			fetchnodedetails.WithInitiator("192.168.1.1"),
		)
		Ω(err).ShouldNot(HaveOccurred())
	})
})
