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
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
)

var TestNewHosts = []string{"pod-4", "pod-5"}
var TestNodes = map[string]string{
	fmt.Sprintf("v_%s_node0001", TestDBName): "pod-1",
	fmt.Sprintf("v_%s_node0002", TestDBName): "pod-2",
	fmt.Sprintf("v_%s_node0003", TestDBName): "pod-3",
}

// mock version of VAddNode() that is invoked inside VClusterOps.AddNode()
func (m *MockVClusterOps) VAddNode(options *vops.VAddNodeOptions) (vops.VCoordinationDatabase, error) {
	vdb := vops.VCoordinationDatabase{}
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}
	// verify basic options
	if !reflect.DeepEqual(options.NewHosts, TestNewHosts) {
		return vdb, fmt.Errorf("failed to retrieve hosts to add")
	}
	if !reflect.DeepEqual(options.Nodes, TestNodes) {
		return vdb, fmt.Errorf("failed to retrieve hosts to add")
	}
	if *options.SCName != TestSCName {
		return vdb, fmt.Errorf("failed to retrieve subcluster name")
	}
	if !*options.SkipRebalanceShards {
		return vdb, fmt.Errorf("SkipRebalanceShards must be true")
	}
	return vdb, nil
}

var _ = Describe("add_node_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with add_node task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		opts := []addnode.Option{
			addnode.WithNodes(TestNodes),
			addnode.WithSubcluster(TestSCName),
		}
		for _, n := range TestNewHosts {
			opts = append(opts, addnode.WithHost(n))
		}
		Ω(dispatcher.AddNode(ctx, opts...)).Should(Succeed())
	})
})
