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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/setconfigparameter"
)

// mock version of VSetConfigurationParameters() that is invoked inside VClusterOps.SetConfigurationParameter()
func (m *MockVClusterOps) VSetConfigurationParameters(options *vops.VSetConfigurationParameterOptions) (err error) {
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

	err = m.VerifySetConfigurationParameterOptions(options)
	if err != nil {
		return err
	}

	return nil
}

var _ = Describe("set_config_parameter_vc", func() {
	ctx := context.Background()

	It("should call VSetConfigurationParameters in the vcluster-ops library", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "set-config-parameter-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		err := dispatcher.SetConfigurationParameter(ctx,
			setconfigparameter.WithUserName(vapi.SuperUser),
			setconfigparameter.WithInitiatorIP(TestSourceIP),
			setconfigparameter.WithSandbox(TestConfigParamSandbox),
			setconfigparameter.WithConfigParameter(TestConfigParamName),
			setconfigparameter.WithValue(TestConfigParamValue),
			setconfigparameter.WithLevel(TestConfigParamLevel),
		)
		Ω(err).Should(Succeed())
	})
})
