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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/unsandboxsc"
)

// mock version of VUnsandbox() that is invoked inside VClusterOps.UnsandboxSubcluster()
func (m *MockVClusterOps) VUnsandbox(options *vops.VUnsandboxOptions) error {
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

	if options.RestartSC != false {
		return fmt.Errorf("failed to set the options to let vcluster not auto-restart the subcluster")
	}

	// verify auth options
	return m.VerifyCerts(&options.DatabaseOptions)
}

var _ = Describe("unsandbox_sc_vc", func() {
	ctx := context.Background()

	It("should call vclusterOps library with unsandbox_subcluster task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = TestNMATLSSecret
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		Ω(dispatcher.UnsandboxSubcluster(ctx,
			unsandboxsc.WithInitiator(TestInitiatorIP),
			unsandboxsc.WithSubcluster(TestSCName))).Should(Succeed())
	})
})
