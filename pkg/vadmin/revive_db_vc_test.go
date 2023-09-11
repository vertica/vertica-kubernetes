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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	TestDescribeOutput = "<sample data coming back>"
)

// mock version of VReviveDatabase() that is invoked inside VClusterOps
func (m *MockVClusterOps) VReviveDatabase(options *vops.VReviveDatabaseOptions) (string, error) {
	// verify basic options
	err := m.VerifyDBNameAndIPv6(&options.DatabaseOptions)
	if err != nil {
		return "", err
	}

	// If running with display only, we only use a single host.
	var expectedHosts = TestHosts
	if *options.DisplayOnly {
		expectedHosts = []string{TestHosts[0]}
	}
	err = m.VerifyHosts(&options.DatabaseOptions, expectedHosts)
	if err != nil {
		return "", err
	}
	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return "", err
	}
	err = m.VerifyCommunalStorageOptions(*options.CommunalStorageLocation, options.CommunalStorageParameters)
	if err != nil {
		return "", err
	}
	if *options.DisplayOnly {
		return TestDescribeOutput, nil
	}
	return "", nil
}

var _ = Describe("revive_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with revive_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.HTTPServerTLSSecret = "revive-db-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)

		ctrlRes, err := dispatcher.ReviveDB(ctx,
			revivedb.WithHosts(TestHosts),
			revivedb.WithCommunalPath(TestCommunalPath),
			revivedb.WithConfigurationParams(TestCommunalStorageParams))
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
