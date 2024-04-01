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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

const TestCatalogPrefix = "/data"
const TestTimeout = 10

// mock version of VStartDatabase() that is invoked inside VClusterOps.StartDB()
func (m *MockVClusterOps) VStartDatabase(options *vops.VStartDatabaseOptions) (*vops.VCoordinationDatabase, error) {
	// verify basic options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}

	// verify eon options
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}
	err = m.VerifyCommunalStorageOptions(*options.CommunalStorageLocation, options.ConfigurationParameters)
	if err != nil {
		return nil, err
	}

	// verify catalog prefix
	if *options.CatalogPrefix != TestCatalogPrefix {
		return nil, fmt.Errorf("failed to retrieve catalog prefix")
	}

	// verify timeout
	if *options.StatePollingTimeout != TestTimeout {
		return nil, fmt.Errorf("failed to retrieve timeout")
	}

	return nil, nil
}

var _ = Describe("start_db_vc", func() {
	ctx := context.Background()

	var nodeIPs []string
	for i := 0; i < 3; i++ {
		nodeIP := fmt.Sprintf("10.10.10.1%d", i)
		nodeIPs = append(nodeIPs, nodeIP)
	}

	It("should call vcluster-ops library with start_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Annotations[vmeta.RestartTimeoutAnnotation] = "10"
		dispatcher.VDB.Spec.NMATLSSecret = "start-db-test-secret"
		dispatcher.VDB.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		ctrlRes, err := dispatcher.StartDB(ctx,
			startdb.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
			startdb.WithHost(nodeIPs[0]),
			startdb.WithHost(nodeIPs[1]),
			startdb.WithHost(nodeIPs[2]),
			startdb.WithCommunalPath(TestCommunalPath),
			startdb.WithConfigurationParams(TestCommunalStorageParams))
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
