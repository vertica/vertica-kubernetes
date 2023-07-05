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
	"reflect"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

var TestHosts = []string{"pod-1", "pod-2", "pod-3"}

const (
	TestCommunalPath       = "/communal"
	TestCatalogPath        = "/catalog"
	TestDepotPath          = "/depot"
	TestDataPath           = "/data"
	TestLicensePath        = "/root/license.key"
	TestShardCount         = 11
	TestSkipPackageInstall = true
)

// mock version of VCreateDatabase() that is invoked inside VClusterOps.CreateDB()
func (m *MockVClusterOps) VCreateDatabase(options *vops.VCreateDatabaseOptions) (vops.VCoordinationDatabase, error) {
	vdb := vops.VCoordinationDatabase{}

	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}

	// verify basic options
	if !reflect.DeepEqual(options.RawHosts, TestHosts) {
		return vdb, fmt.Errorf("failed to retrieve hosts")
	}
	if *options.CommunalStorageLocation != TestCommunalPath {
		return vdb, fmt.Errorf("failed to retrieve communal path")
	}
	if *options.CatalogPrefix != TestCatalogPath {
		return vdb, fmt.Errorf("failed to retrieve catalog path")
	}
	if *options.DepotPrefix != TestDepotPath {
		return vdb, fmt.Errorf("failed to retrieve depot path")
	}
	if *options.DataPrefix != TestDataPath {
		return vdb, fmt.Errorf("failed to retrieve data path")
	}
	if *options.LicensePathOnNode != TestLicensePath {
		return vdb, fmt.Errorf("failed to retrieve license path")
	}
	if *options.ShardCount != TestShardCount {
		return vdb, fmt.Errorf("failed to retrieve shard count")
	}
	if *options.SkipPackageInstall != TestSkipPackageInstall {
		return vdb, fmt.Errorf("failed to retrieve SkipPackageInstall")
	}

	// verify auth options
	if options.Key != test.TestKeyValue {
		return vdb, fmt.Errorf("failed to load key")
	}
	if options.Cert != test.TestCertValue {
		return vdb, fmt.Errorf("failed to load cert")
	}
	if options.CaCert != test.TestCaCertValue {
		return vdb, fmt.Errorf("failed to load ca cert")
	}

	// vdb.Name is used in VClusterOps.CreateDB() so we give it a value
	vdb.Name = TestDBName
	return vdb, nil
}

var _ = Describe("create_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with create_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPServerTLSSecret)
		Ω(dispatcher.CreateDB(ctx,
			createdb.WithHosts(TestHosts),
			createdb.WithDBName(TestDBName),
			createdb.WithCommunalPath(TestCommunalPath),
			createdb.WithCatalogPath(TestCatalogPath),
			createdb.WithDepotPath(TestDepotPath),
			createdb.WithDataPath(TestDataPath),
			createdb.WithLicensePath(TestLicensePath),
			createdb.WithShardCount(TestShardCount),
			createdb.WithSkipPackageInstall())).Should(Equal(ctrl.Result{}))
	})
})
