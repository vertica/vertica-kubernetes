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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

const TestCatalogPrefix = "/data"

// mock version of VStartDatabase() that is invoked inside VClusterOps.StartDB()
func (m *MockVClusterOps) VStartDatabase(options *vops.VStartDatabaseOptions) error {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify eon options
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify catalog prefix
	if *options.CatalogPrefix != TestCatalogPrefix {
		return fmt.Errorf("failed to retrieve catalog prefix")
	}

	return nil
}

var _ = Describe("start_db_vc", func() {
	ctx := context.Background()

	var nodeIPs []string
	for i := 1; i <= 3; i++ {
		nodeIP := fmt.Sprintf("192.168.1.%d", i)
		nodeIPs = append(nodeIPs, nodeIP)
	}

	It("should call vcluster-ops library with start_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		ctrlRes, err := dispatcher.StartDB(ctx,
			startdb.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
			startdb.WithHost(nodeIPs[0]),
			startdb.WithHost(nodeIPs[1]),
			startdb.WithHost(nodeIPs[2]))
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
