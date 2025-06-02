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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	"golang.org/x/exp/maps"
	ctrl "sigs.k8s.io/controller-runtime"
)

var TestHosts = []string{"pod-1", "pod-2", "pod-3"}

const (
	TestCatalogPath               = "/catalog"
	TestDepotPath                 = "/depot"
	TestDataPath                  = "/data"
	TestLicensePath               = "/root/license.key"
	TestShardCount                = 11
	TestSkipPackageInstall        = true
	TestTimeoutNodeStartupSeconds = 600
	TestTLSMode                   = "VERIFY_CA"
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
	err = m.VerifyHosts(&options.DatabaseOptions, TestHosts)
	if err != nil {
		return vdb, err
	}
	err = m.VerifyCommunalStorageOptions(options.CommunalStorageLocation, options.ConfigurationParameters)
	if err != nil {
		return vdb, err
	}
	err = m.verifyPaths(options)
	if err != nil {
		return vdb, err
	}
	if options.ShardCount != TestShardCount {
		return vdb, fmt.Errorf("failed to retrieve shard count")
	}
	if options.SkipPackageInstall != TestSkipPackageInstall {
		return vdb, fmt.Errorf("failed to retrieve SkipPackageInstall")
	}

	// verify auth options
	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return vdb, err
	}

	// vdb.Name is used in VClusterOps.CreateDB() so we give it a value
	vdb.Name = TestDBName

	if m.ReturnDBIsRunning {
		return vdb, &vops.DBIsRunningError{Detail: "db is already running"}
	}

	// verify TimeoutNodeStartupSeconds
	if m.VerifyTimeoutNodeStartupSeconds && options.TimeoutNodeStartupSeconds != TestTimeoutNodeStartupSeconds {
		return vdb, fmt.Errorf("fail to read TimeoutNodeStartupSeconds from annotations: %d", options.TimeoutNodeStartupSeconds)
	}

	if len(options.HTTPSTLSConfiguration) > 0 {
		configMap := genTLSConfigurationMap(TestTLSMode, TestNMATLSSecret, "default")
		if !maps.Equal(options.HTTPSTLSConfiguration, configMap) {
			return vdb, fmt.Errorf("https tls configuration not valid")
		}
	}

	return vdb, nil
}

func (m *MockVClusterOps) verifyPaths(options *vops.VCreateDatabaseOptions) error {
	if options.CatalogPrefix != TestCatalogPath {
		return fmt.Errorf("failed to retrieve catalog path")
	}
	if options.DepotPrefix != TestDepotPath {
		return fmt.Errorf("failed to retrieve depot path")
	}
	if options.DataPrefix != TestDataPath {
		return fmt.Errorf("failed to retrieve data path")
	}
	if options.LicensePathOnNode != TestLicensePath {
		return fmt.Errorf("failed to retrieve license path")
	}

	return nil
}

var _ = Describe("create_db_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with create_db task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.HTTPSNMATLSSecret = TestNMATLSSecret
		dispatcher.VDB.Spec.HTTPSTLSMode = TestTLSMode
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		Ω(callCreateDB(ctx, dispatcher)).Should(Equal(ctrl.Result{}))

		vapi.SetVDBForTLS(dispatcher.VDB)
		Ω(callCreateDB(ctx, dispatcher)).Should(Equal(ctrl.Result{}))
	})

	It("should detect DBIsRunningError", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLSSecret = TestNMATLSSecret
		vdb.Annotations[vmeta.FailCreateDBIfVerticaIsRunningAnnotation] = vmeta.FailCreateDBIfVerticaIsRunningAnnotationTrue
		setupAPIFunc := func(logr.Logger, string) (VClusterProvider, logr.Logger) {
			return &MockVClusterOps{ReturnDBIsRunning: true}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		_, err := callCreateDB(ctx, dispatcher)
		Ω(err).ShouldNot(Succeed())
		dbIsRunningError := &vops.DBIsRunningError{}
		ok := errors.As(err, &dbIsRunningError)
		Ω(ok).Should(BeTrue())

		vdb.Annotations[vmeta.FailCreateDBIfVerticaIsRunningAnnotation] = vmeta.FailCreateDBIfVerticaIsRunningAnnotationFalse
		Ω(callCreateDB(ctx, dispatcher)).Should(Equal(ctrl.Result{}))
	})

	It("should detect TimeoutNodeStartupSeconds", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPSNMATLSSecret = TestNMATLSSecret
		vdb.Annotations[vmeta.CreateDBTimeoutAnnotation] = fmt.Sprint(TestTimeoutNodeStartupSeconds)
		Ω(vdb.GetCreateDBNodeStartTimeout()).Should(Equal(TestTimeoutNodeStartupSeconds))
		setupAPIFunc := func(logr.Logger, string) (VClusterProvider, logr.Logger) {
			return &MockVClusterOps{VerifyTimeoutNodeStartupSeconds: true}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLSSecret)
		Ω(callCreateDB(ctx, dispatcher)).Should(Equal(ctrl.Result{}))
	})
})

// callCreateDB is a helper to call the create db interface with the standard test inputs
func callCreateDB(ctx context.Context, dispatcher *VClusterOps) (ctrl.Result, error) {
	return dispatcher.CreateDB(ctx,
		createdb.WithHosts(TestHosts),
		createdb.WithDBName(TestDBName),
		createdb.WithCommunalPath(TestCommunalPath),
		createdb.WithConfigurationParams(TestCommunalStorageParams),
		createdb.WithCatalogPath(TestCatalogPath),
		createdb.WithDepotPath(TestDepotPath),
		createdb.WithDataPath(TestDataPath),
		createdb.WithLicensePath(TestLicensePath),
		createdb.WithShardCount(TestShardCount),
		createdb.WithSkipPackageInstall())
}
