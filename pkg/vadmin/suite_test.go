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
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var k8sClient client.Client
var testEnv *envtest.Environment
var logger logr.Logger
var restCfg *rest.Config

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
	logf.SetLogger(logger)

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	cfg, err := testEnv.Start()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
	ExpectWithOffset(1, cfg).NotTo(BeNil())
	restCfg = cfg

	err = vapi.AddToScheme(scheme.Scheme)
	ExpectWithOffset(1, err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restCfg, client.Options{Scheme: scheme.Scheme})
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	ExpectWithOffset(1, err).NotTo(HaveOccurred())
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vadmin Suite")
}

// mockAdmintoolsDispatcher will create an admintools dispatcher for test purposes
func mockAdmintoolsDispatcher() (*Admintools, *vapi.VerticaDB, *cmds.FakePodRunner) {
	vdb := vapi.MakeVDB()
	fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
	evWriter := aterrors.TestEVWriter{}
	dispatcher := MakeAdmintools(logger, vdb, fpr, &evWriter, false)
	return dispatcher.(*Admintools), vdb, fpr
}

// MockVClusterOps is used to invoke mock vcluster-ops functions
type MockVClusterOps struct {
}

// const variables used for vcluster-ops unit test
const (
	TestDBName       = "test-db"
	TestPassword     = "test-pw"
	TestIPv6         = false
	TestParm         = "Parm1"
	TestValue        = "val1"
	TestInitiatorIP  = "10.10.10.10"
	TestIsEon        = true
	TestCommunalPath = "/communal"
)

var TestCommunalStorageParams = map[string]string{"awsauth": "test-auth", "awsconnecttimeout": "10"}

// VerifyDBNameAndIPv6 is used in vcluster-ops unit test for verifying db name and ipv6
func (m *MockVClusterOps) VerifyDBNameAndIPv6(options *vops.DatabaseOptions) error {
	if options.Ipv6.ToBool() != TestIPv6 {
		return fmt.Errorf("failed to retrieve IPv6")
	}
	if *options.DBName != TestDBName {
		return fmt.Errorf("failed to retrieve database name")
	}

	return nil
}

// VerifyCommonOptions is used in vcluster-ops unit test for verifying the common options among all db ops
func (m *MockVClusterOps) VerifyCommonOptions(options *vops.DatabaseOptions) error {
	// verify basic options
	err := m.VerifyDBNameAndIPv6(options)
	if err != nil {
		return err
	}

	// verify auth options
	if *options.UserName != vapi.SuperUser {
		return fmt.Errorf("failed to retrieve Vertica username")
	}
	if *options.Password != TestPassword {
		return fmt.Errorf("failed to retrieve Vertica password")
	}

	return nil
}

// VerifyInitiatorIPAndEonMode is used in vcluster-ops unit test for verifying initiator ip and eon mode.
// They are the common options in some vcluster commands like stop_db and db_add_subcluster
func (m *MockVClusterOps) VerifyInitiatorIPAndEonMode(options *vops.DatabaseOptions) error {
	// verify initiator ip
	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return fmt.Errorf("failed to retrieve hosts")
	}

	// verify eon mode
	if options.IsEon.ToBool() != TestIsEon {
		return fmt.Errorf("failed to retrieve eon mode")
	}

	return nil
}

// VerifyHosts is used in vcluster-ops unit test for verifying hosts
func (m *MockVClusterOps) VerifyHosts(options *vops.DatabaseOptions, hosts []string) error {
	if !reflect.DeepEqual(options.RawHosts, hosts) {
		return fmt.Errorf("failed to retrieve hosts '%v' in '%v'", hosts, options.RawHosts)
	}

	return nil
}

// VerifyCommunalStorageOptions is used in vcluster-ops unit test for verifying communal storage options
func (m *MockVClusterOps) VerifyCommunalStorageOptions(communalStoragePath string, configurationParams map[string]string) error {
	if communalStoragePath != TestCommunalPath {
		return fmt.Errorf("failed to retrieve communal storage path")
	}

	if !reflect.DeepEqual(configurationParams, TestCommunalStorageParams) {
		return fmt.Errorf("failed to retrieve configuration params")
	}

	return nil
}

// VerifyCerts is used in vcluster-ops unit test for verifying key and certs
func (m *MockVClusterOps) VerifyCerts(options *vops.DatabaseOptions) error {
	if options.Key != test.TestKeyValue {
		return fmt.Errorf("failed to load key")
	}
	if options.Cert != test.TestCertValue {
		return fmt.Errorf("failed to load cert")
	}
	if options.CaCert != test.TestCaCertValue {
		return fmt.Errorf("failed to load ca cert")
	}

	return nil
}

// mockVClusterOpsDispatcher will create an vcluster-ops dispatcher for test purposes
func mockVClusterOpsDispatcher() *VClusterOps {
	vdb := vapi.MakeVDB()
	vdb.Spec.NMATLSSecret = "test-secret"
	evWriter := aterrors.TestEVWriter{}
	// We use a function to construct the VClusterProvider. This is called
	// ahead of each API rather than once so that we can setup a custom
	// logger for each API call.
	setupAPIFunc := func(log logr.Logger, apiName string) (VClusterProvider, logr.Logger) {
		return &MockVClusterOps{}, logr.Logger{}
	}
	dispatcher := MakeVClusterOps(logger, vdb, k8sClient, TestPassword, &evWriter, setupAPIFunc)
	return dispatcher.(*VClusterOps)
}

func createNonEmptyFileHelper(res ctrl.Result, err error, fpr *cmds.FakePodRunner) {
	Ω(err).Should(Succeed())
	Ω(res).Should(Equal(ctrl.Result{}))
	hist := fpr.FindCommands("cat >")
	Ω(len(hist)).Should(Equal(1))
	expContent := fmt.Sprintf("%s = %s\n", TestParm, TestValue)
	Expect(hist[0].Command).Should(ContainElement(fmt.Sprintf("cat > %s<<< '%s'", paths.AuthParmsFile, expContent)))
}
