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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mock version of VReIP() that is invoked inside VClusterOps.ReIP()
func (m *MockVClusterOps) VReIP(options *vops.VReIPOptions) error {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify catalog path
	if options.CatalogPrefix != TestCatalogPrefix {
		return fmt.Errorf("failed to retrieve catalog prefix")
	}

	// verify re-ip list
	if len(options.ReIPList) == 0 {
		return fmt.Errorf("the re-ip list should not be empty")
	}

	// verify eon options
	if options.IsEon != TestIsEon {
		return fmt.Errorf("failed to retrieve eon mode")
	}

	// verify sandbox name
	if options.SandboxName != TestSandboxName {
		return fmt.Errorf("failed to retrieve sandbox name")
	}
	return m.VerifyCommunalStorageOptions(options.CommunalStorageLocation, options.ConfigurationParameters)
}

var _ = Describe("re_ip_vc", func() {
	ctx := context.Background()

	var hosts []reip.Host
	for i := 1; i <= 3; i++ {
		var h reip.Host
		h.IP = fmt.Sprintf("192.168.1.%d", i)
		h.Compat21Node = fmt.Sprintf("node000%d", i)
		h.VNode = fmt.Sprintf("v_%s_%s", TestDBName, h.Compat21Node)

		hosts = append(hosts, h)
	}

	It("should call vcluster-ops library with re_ip task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = "re-ip-test-secret"
		dispatcher.VDB.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)

		ctrlRes, err := dispatcher.ReIP(ctx,
			reip.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), hosts[0].IP),
			reip.WithHost(hosts[0].VNode, hosts[0].Compat21Node, hosts[0].IP),
			reip.WithHost(hosts[1].VNode, hosts[1].Compat21Node, hosts[1].IP),
			reip.WithHost(hosts[1].VNode, hosts[1].Compat21Node, hosts[1].IP),
			reip.WithCommunalPath(TestCommunalPath),
			reip.WithConfigurationParams(TestCommunalStorageParams),
			reip.WithSandbox(TestSandboxName))
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
