/*
 (c) Copyright [2021-2023] Open Text.
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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
)

const (
	TestInitiatorIP = "10.10.10.10"
	TestIsEon       = true
)

// mock version of VStopDatabase() that is invoked inside VClusterOps.StopDB()
func (m *MockVClusterOps) VStopDatabase(options *vops.VStopDatabaseOptions) error {
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify input hosts
	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return fmt.Errorf("failed to retrieve hosts")
	}
	if options.IsEon.ToBool() != TestIsEon {
		return fmt.Errorf("failed to retrieve eon mode")
	}

	return nil
}

var _ = Describe("stop_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with stop_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		Ω(dispatcher.StopDB(ctx, stopdb.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP))).Should(Succeed())
	})
})
