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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/saverestorepoint"
)

const (
	sb = "sand"
)

// mock version of VSaveRestorePoint() that is invoked inside VClusterOps.VSaveRestorePoint()
func (m *MockVClusterOps) VSaveRestorePoint(options *vops.VSaveRestorePointOptions) error {
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
	if options.ArchiveName != TestArchiveName {
		return fmt.Errorf("failed to retrieve archive name")
	}

	if options.Sandbox != sb {
		return fmt.Errorf("failed to retrieve sandbox")
	}

	// verify auth options
	return m.VerifyCerts(&options.DatabaseOptions)
}

var _ = Describe("save_restore_point_vc", func() {
	ctx := context.Background()

	It("should call vclusterOps library with save_restore_point task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "save-restore-point"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		Ω(dispatcher.SaveRestorePoint(ctx,
			saverestorepoint.WithInitiator(TestInitiatorIP),
			saverestorepoint.WithSandbox(sb),
			saverestorepoint.WithArchiveName(TestArchiveName))).Should(Succeed())
	})
})
