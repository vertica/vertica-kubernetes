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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/dropdb"
)

// mock version of VDropDatabase() that is invoked inside VClusterOps
func (m *MockVClusterOps) VDropDatabase(options *vops.VDropDatabaseOptions) error {
	// verify basic options
	err := m.VerifyDBNameAndIPv6(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	for i := range options.NodesToDrop {
		node := &options.NodesToDrop[i]
		if node.Address != fmt.Sprintf("192.168.1.%d", i+1) {
			return fmt.Errorf("failed to retrieve node address")
		}
		nodeName := fmt.Sprintf("v_%s_%s%d", TestDBName, "node000", i+1)
		if node.Name != nodeName {
			return fmt.Errorf("failed to retrieve node name")
		}
		if node.CatalogPath != fmt.Sprintf("%s/%s/%s_catalog", TestCatalogPath, TestDBName, nodeName) {
			return fmt.Errorf("failed to retrieve node catalog path")
		}
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
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = "drop-db-test-secret"
		dispatcher.VDB.Spec.Local.CatalogPath = TestCatalogPath
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		dispatcher.VDB.Annotations = map[string]string{vmeta.PreserveDBDirectoryAnnotation: "true"}

		var hosts []dropdb.Host
		for i := 1; i <= 3; i++ {
			var h dropdb.Host
			h.IP = fmt.Sprintf("192.168.1.%d", i)
			h.VNode = fmt.Sprintf("v_%s_%s%d", TestDBName, "node000", i)
			hosts = append(hosts, h)
		}

		err := dispatcher.DropDB(ctx,
			dropdb.WithHost(hosts[0].VNode, hosts[0].IP),
			dropdb.WithHost(hosts[1].VNode, hosts[1].IP),
			dropdb.WithHost(hosts[2].VNode, hosts[2].IP),
			dropdb.WithDBName(TestDBName))
		Ω(err).Should(Succeed())
	})
})
