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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/manageconnectiondraining"
)

// mock version of VManageConnectionDraining() that is invoked inside VClusterOps.ManageConnectionDraining()
func (m *MockVClusterOps) VManageConnectionDraining(options *vops.VManageConnectionDrainingOptions) error {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
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

	return nil
}

var _ = Describe("manage_connection_draining_vc", func() {
	ctx := context.Background()

	It("should call VManageConnectionDraining in the vcluster-ops library", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "manage-conn-drain-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		err := dispatcher.ManageConnectionDraining(ctx,
			manageconnectiondraining.WithInitiator(TestSourceIP),
			manageconnectiondraining.WithSandbox(TestConfigParamSandbox),
			manageconnectiondraining.WithSubcluster(TestSCName),
		)
		Ω(err).Should(Succeed())
	})
})
