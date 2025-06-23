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
	"maps"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/settlsconfig"
)

// mock version of VSetTLSConfig() that is invoked inside VClusterOps.SetTLSConfig()
func (m *MockVClusterOps) VSetTLSConfig(options *vops.VSetTLSConfigOptions) (err error) {
	// verify basic options
	err = m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return fmt.Errorf("failed to retrieve hosts")
	}

	configMap := genTLSConfigurationMap(TestHTTPSTLSMode, TestNMATLSSecret, TestNamespace)
	if !maps.Equal(options.HTTPSTLSConfig.ConfigMap, configMap) {
		return fmt.Errorf("https tls configuration not valid")
	}
	configMap = genTLSConfigurationMap(TestClientServerTLSMode, TestClientServerSecret, TestNamespace)
	if !maps.Equal(options.ServerTLSConfig.ConfigMap, configMap) {
		return fmt.Errorf("client server tls configuration not valid")
	}

	return nil
}

var _ = Describe("set_config_parameter_vc", func() {
	ctx := context.Background()

	It("should call VSetConfigurationParameters in the vcluster-ops library", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = TestNMATLSSecret
		dispatcher.VDB.Spec.ClientServerTLS.Secret = TestClientServerSecret
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.ClientServerTLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.ClientServerTLS.Secret)

		err := dispatcher.SetTLSConfig(ctx,
			settlsconfig.WithInitiatorIP(TestSourceIP),
			settlsconfig.WithClientServerTLSMode(TestClientServerTLSMode),
			settlsconfig.WithHTTPSTLSMode(TestHTTPSTLSMode),
			settlsconfig.WithClientServerTLSSecretName(dispatcher.VDB.Spec.ClientServerTLS.Secret),
			settlsconfig.WithHTTPSTLSSecretName(dispatcher.VDB.Spec.NMATLSSecret),
			settlsconfig.WithNamespace(TestNamespace),
		)
		Ω(err).Should(Succeed())
	})
})
