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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
)

const (
	TestSCName    = "sc1"
	TestIsPrimary = false
)

// mock version of VAddSubcluster() that is invoked inside VClusterOps.AddSubcluster()
func (m *MockVClusterOps) VAddSubcluster(options *vops.VAddSubclusterOptions) error {
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify hosts and eon mode
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify basic options
	if options.SCName != TestSCName {
		return fmt.Errorf("failed to retrieve subcluster name")
	}
	if options.IsPrimary != TestIsPrimary {
		return fmt.Errorf("failed to retrieve subcluster type")
	}

	return nil
}

var _ = Describe("add_sc_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with add_subcluster task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.NMATLSSecret = "add-sc-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		dispatcher.VDB.Spec.DBName = TestDBName
		Ω(dispatcher.AddSubcluster(ctx,
			addsc.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP),
			addsc.WithSubcluster(TestSCName),
			addsc.WithIsPrimary(TestIsPrimary))).Should(Succeed())
	})
})
