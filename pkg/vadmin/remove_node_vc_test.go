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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
	"k8s.io/apimachinery/pkg/types"
)

var TestHostsToRemove = []string{"pod-3"}
var TestInitiatorPodName = types.NamespacedName{
	Name:      "pod-1",
	Namespace: "ns",
}
var TestInitiatorPodIP = "10.0.0.1"

// mock version of VRemoveNode() that is invoked inside VClusterOps.RemoveNode()
func (m *MockVClusterOps) VRemoveNode(options *vops.VRemoveNodeOptions) (vops.VCoordinationDatabase, error) {
	vdb := vops.VCoordinationDatabase{}
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}
	// verify basic options
	if !reflect.DeepEqual(options.HostsToRemove, TestHostsToRemove) {
		return vdb, fmt.Errorf("failed to retrieve hosts to remove")
	}
	if !reflect.DeepEqual(options.RawHosts, []string{TestInitiatorPodIP}) {
		return vdb, fmt.Errorf("failed to retrieve initiator")
	}
	return vdb, nil
}

var _ = Describe("remove_node_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with remove_node task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.NmaTLSSecret = "remove-node-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NmaTLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NmaTLSSecret)
		dispatcher.VDB.Spec.DBName = TestDBName
		opts := []removenode.Option{
			removenode.WithInitiator(TestInitiatorPodName, TestInitiatorPodIP),
		}
		for _, n := range TestHostsToRemove {
			opts = append(opts, removenode.WithHost(n))
		}
		Ω(dispatcher.RemoveNode(ctx, opts...)).Should(Succeed())
	})
})
