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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
	"k8s.io/apimachinery/pkg/types"
)

var TestNewHosts = []string{"pod-4", "pod-5"}
var TestPod4Name = types.NamespacedName{
	Name:      "pod-4",
	Namespace: "ns",
}
var TestPod5Name = types.NamespacedName{
	Name:      "pod-5",
	Namespace: "ns",
}
var TestNewPodNames = []types.NamespacedName{TestPod4Name, TestPod5Name}
var testExpectedNodeNames = []string{"v_" + TestDBName + "_node0001",
	"v_" + TestDBName + "_node0002",
	"v_" + TestDBName + "_node0003"}

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
	if *options.SCName != TestSCName {
		return vdb, fmt.Errorf("failed to retrieve subcluster name")
	}
	if !reflect.DeepEqual(options.RawHosts, []string{TestInitiatorPodIP}) {
		return vdb, fmt.Errorf("failed to retrieve initiator")
	}
	if !*options.SkipRebalanceShards {
		return vdb, fmt.Errorf("SkipRebalanceShards must be true")
	}
	if !reflect.DeepEqual(options.ExpectedNodeNames, testExpectedNodeNames) {
		return vdb, fmt.Errorf("fail to retrieve ExpectedNodeNames")
	}
	return vdb, nil
}

var _ = Describe("add_node_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with add_node task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.HTTPServerTLSSecret = "add-node-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		dispatcher.VDB.Spec.DBName = TestDBName
		opts := []addnode.Option{
			addnode.WithInitiator(TestInitiatorPodName, TestInitiatorPodIP),
			addnode.WithSubcluster(TestSCName),
			addnode.WithExpectedNodeNames(testExpectedNodeNames),
		}
		for i, n := range TestNewHosts {
			opts = append(opts, addnode.WithHost(n, TestNewPodNames[i]))
		}
		Ω(dispatcher.AddNode(ctx, opts...)).Should(Succeed())
	})
})
