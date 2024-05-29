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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
)

const sbName = "sb1"

// mock version of VStopDatabase() that is invoked inside VClusterOps.StopDB()
func (m *MockVClusterOps) VStopDatabase(options *vops.VStopDatabaseOptions) error {
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

	if options.SandboxName != sbName {
		return fmt.Errorf("failed to retrieve sandbox name")
	}

	mainCluster := sbName == vapi.MainCluster
	if options.MainCluster != mainCluster {
		return fmt.Errorf("wrong value for MainCluster option")
	}
	return nil
}

var _ = Describe("stop_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with stop_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		opts := []stopdb.Option{
			stopdb.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP),
			stopdb.WithSandbox(sbName),
		}
		Ω(dispatcher.StopDB(ctx, opts...)).Should(Succeed())
	})
})
