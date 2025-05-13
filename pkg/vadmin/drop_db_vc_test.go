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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/dropdb"
	"k8s.io/apimachinery/pkg/types"
)

// mock version of VDropDatabase() that is invoked inside VClusterOps
func (m *MockVClusterOps) VDropDatabase(options *vops.VDropDatabaseOptions) error {
	// verify basic options
	err := m.VerifyDBNameAndIPv6(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	var expectedHosts = TestHosts
	err = m.VerifyHosts(&options.DatabaseOptions, expectedHosts)
	if err != nil {
		return err
	}

	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	if options.RetainCatalogDir != true {
		return fmt.Errorf("drop database needs to retain catalog directory")
	}

	return nil
}

var _ = Describe("drop_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with drop_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "drop-db-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		dispatcher.VDB.Spec.Annotations = map[string]string{"vertica.com/preserve-db-dir": "true"}

		err := dispatcher.DropDB(ctx,
			dropdb.WithInitiator(types.NamespacedName{}),
			dropdb.WithHosts(TestHosts),
			dropdb.WithDBName(TestDBName))
		Ω(err).Should(Succeed())
	})
})
