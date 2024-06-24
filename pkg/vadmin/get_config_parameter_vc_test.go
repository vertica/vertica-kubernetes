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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
)

// mock version of VGetConfigurationParameters() that is invoked inside VClusterOps.GetConfigurationParameter()
func (m *MockVClusterOps) VGetConfigurationParameters(options *vops.VGetConfigurationParameterOptions) (value string, err error) {
	// verify basic options
	err = m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return "", err
	}

	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return "", err
	}

	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return "", fmt.Errorf("failed to retrieve hosts")
	}

	err = m.VerifyGetConfigurationParameterOptions(options)
	if err != nil {
		return "", err
	}

	return TestConfigParamValue, nil
}

var _ = Describe("get_config_parameter_vc", func() {
	ctx := context.Background()

	It("should call VGetConfigurationParameters in the vcluster-ops library", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "get-config-parameter-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		value, err := dispatcher.GetConfigurationParameter(ctx,
			getconfigparameter.WithUserName(vapi.SuperUser),
			getconfigparameter.WithInitiatorIP(TestSourceIP),
			getconfigparameter.WithSandbox(TestConfigParamSandbox),
			getconfigparameter.WithConfigParameter(TestConfigParamName),
			getconfigparameter.WithLevel(TestConfigParamLevel),
		)
		Ω(err).Should(Succeed())
		Ω(value).Should(Equal(TestConfigParamValue))
	})
})
