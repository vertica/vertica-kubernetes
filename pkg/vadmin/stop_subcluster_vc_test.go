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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsubcluster"
)

const scName = "sc1"

// mock version of VStopDatabase() that is invoked inside VClusterOps.StopDB()
func (m *MockVClusterOps) VStopSubcluster(options *vops.VStopSubclusterOptions) error {
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

	if options.SCName != scName {
		return fmt.Errorf("failed to retrieve subcluster name")
	}
	return nil
}

var _ = Describe("stop_subcluster_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with stop_subcluster task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		opts := []stopsubcluster.Option{
			stopsubcluster.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP),
			stopsubcluster.WithSCName(scName),
		}
		Ω(dispatcher.StopSubcluster(ctx, opts...)).Should(Succeed())
	})
})
