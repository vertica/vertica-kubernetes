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
	dispatcher := MakeAdmintools(logger, vdb, fpr, &evWriter)
	return dispatcher.(*Admintools), vdb, fpr
}

// MockVClusterOps is used to invoke mock vcluster-ops functions
type MockVClusterOps struct {
	// Set this to true if you want VCreateDatabase to return the DBIsRunningError
	ReturnDBIsRunning               bool
	VerifyTimeoutNodeStartupSeconds bool
	// Set this to true if you want VStartNodes to return the ReIPNoClusterQuorumError
	ReturnReIPNoClusterQuorum bool
}

// const variables used for vcluster-ops unit test
const (
	TestDBName             = "test-db"
	TestTargetDBName       = "test-target-db"
	TestPassword           = "test-pw"
	TestTargetPassword     = "test-target-pw"
	TestTargetUserName     = "test-target-user"
	TestIPv6               = false
	TestParm               = "Parm1"
	TestValue              = "val1"
	TestInitiatorIP        = "10.10.10.10"
	TestSourceIP           = "10.10.10.10"
	TestTargetIP           = "10.10.10.11"
	TestSourceTLSConfig    = "test-tls-config"
	TestIsEon              = true
	TestCommunalPath       = "/communal"
	TestNMATLSSecret       = "test-secret"
	TestArchiveName        = "test-archive-name"
	TestStartTimestamp     = "2006-01-02"
	TestEndTimestamp       = "2006-01-02 15:04:05"
	TestConfigParamSandbox = "test-config-param-sandbox"
	TestConfigParamName    = "test-config-param-name"
	TestConfigParamValue   = "test-config-param-value"
	TestConfigParamLevel   = "test-config-param-level"
)

var TestCommunalStorageParams = map[string]string{"awsauth": "test-auth", "awsconnecttimeout": "10"}
var TestNodeNameAddressMap = map[string]string{"v_sandbox_db_node0010": "10.244.0.134"}

// VerifyDBNameAndIPv6 is used in vcluster-ops unit test for verifying db name and ipv6
func (m *MockVClusterOps) VerifyDBNameAndIPv6(options *vops.DatabaseOptions) error {
	if options.IPv6 != TestIPv6 {
		return fmt.Errorf("failed to retrieve IPv6")
	}
	if options.DBName != TestDBName {
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
	if options.UserName != vapi.SuperUser {
		return fmt.Errorf("failed to retrieve Vertica username")
	}
	if *options.Password != TestPassword {
		return fmt.Errorf("failed to retrieve Vertica password")
	}

	return nil
}

// VerifyTargetDBNameUserNamePassword is used in vcluster-ops unit test for verifying the target db name,
// username and password in a replication
func (m *MockVClusterOps) VerifyTargetDBNameUserNamePassword(options *vops.VReplicationDatabaseOptions) error {
	if options.TargetDB != TestTargetDBName {
		return fmt.Errorf("failed to retrieve target db name")
	}
	if options.TargetUserName != TestTargetUserName {
		return fmt.Errorf("failed to retrieve target username")
	}
	if options.SourceTLSConfig != "" {
		if options.TargetPassword != nil {
			return fmt.Errorf("target password is not nil when source TLS config is set")
		}
	} else {
		if *options.TargetPassword != TestTargetPassword {
			return fmt.Errorf("failed to retrieve target password")
		}
	}
	return nil
}

// VerifyEonMode is used in vcluster-ops unit test for verifying eon mode
func (m *MockVClusterOps) VerifyEonMode(options *vops.DatabaseOptions) error {
	if options.IsEon != TestIsEon {
		return fmt.Errorf("failed to retrieve eon mode")
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
	return m.VerifyEonMode(options)
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

func (m *MockVClusterOps) VerifyFilterOptions(options *vops.ShowRestorePointFilterOptions) error {
	if options == nil {
		return fmt.Errorf("failed to retrieve filter options")
	}
	if options.ArchiveName == "" || options.ArchiveName != TestArchiveName {
		return fmt.Errorf("failed to retrieve archive name filter")
	}
	if options.StartTimestamp == "" || options.StartTimestamp != TestStartTimestamp {
		return fmt.Errorf("failed to retrieve start timestamp filter")
	}
	if options.EndTimestamp == "" || options.EndTimestamp != TestEndTimestamp {
		return fmt.Errorf("failed to retrieve end timestamp filter")
	}
	return nil
}

func (m *MockVClusterOps) VerifySetConfigurationParameterOptions(options *vops.VSetConfigurationParameterOptions) error {
	if options == nil {
		return fmt.Errorf("failed to retrieve set configuration parameter options")
	}
	if options.Sandbox == "" || options.Sandbox != TestConfigParamSandbox {
		return fmt.Errorf("failed to retrieve sandbox")
	}
	if options.ConfigParameter == "" || options.ConfigParameter != TestConfigParamName {
		return fmt.Errorf("failed to retrieve config param")
	}
	if options.Value == "" || options.Value != TestConfigParamValue {
		return fmt.Errorf("failed to retrieve value")
	}
	if options.Level == "" || options.Level != TestConfigParamLevel {
		return fmt.Errorf("failed to retrieve level")
	}
	return nil
}

func (m *MockVClusterOps) VerifyGetConfigurationParameterOptions(options *vops.VGetConfigurationParameterOptions) error {
	if options == nil {
		return fmt.Errorf("failed to retrieve get configuration parameter options")
	}
	if options.Sandbox == "" || options.Sandbox != TestConfigParamSandbox {
		return fmt.Errorf("failed to retrieve sandbox")
	}
	if options.ConfigParameter == "" || options.ConfigParameter != TestConfigParamName {
		return fmt.Errorf("failed to retrieve config param")
	}
	if options.Level == "" || options.Level != TestConfigParamLevel {
		return fmt.Errorf("failed to retrieve level")
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

func (m *MockVClusterOps) VerifyDBOptions(options *vops.DatabaseOptions) error {
	// verify common options
	err := m.VerifyCommonOptions(options)
	if err != nil {
		return err
	}

	// verify hosts and eon mode
	err = m.VerifyInitiatorIPAndEonMode(options)
	if err != nil {
		return err
	}

	// verify auth options
	return m.VerifyCerts(options)
}

// VerifySourceAndTargetIPs is used in vcluster-ops unit test for verifying source and target hosts
// (both a single IP) in a replication
func (m *MockVClusterOps) VerifySourceAndTargetIPs(options *vops.VReplicationDatabaseOptions) error {
	if len(options.RawHosts) != 1 || options.RawHosts[0] != TestSourceIP {
		return fmt.Errorf("failed to load source IP")
	}
	if len(options.TargetHosts) != 1 || options.TargetHosts[0] != TestTargetIP {
		return fmt.Errorf("failed to load target IP")
	}
	return nil
}

// VerifySourceTLSConfig is used in vcluster-ops unit test for verifying source TLS config
func (m *MockVClusterOps) VerifySourceTLSConfig(options *vops.VReplicationDatabaseOptions) error {
	if options.SourceTLSConfig != TestSourceTLSConfig {
		return fmt.Errorf("failed to load source TLS config")
	}
	return nil
}

func (m *MockVClusterOps) VPollSubclusterState(_ *vops.VPollSubclusterStateOptions) error {
	return nil
}

// mockVClusterOpsDispatcher will create an vcluster-ops dispatcher for test
// purposes. This uses a standard function to setup the API.
func mockVClusterOpsDispatcher() *VClusterOps {
	vdb := vapi.MakeVDB()
	vdb.Spec.NMATLSSecret = "test-secret"
	// We use a function to construct the VClusterProvider. This is called
	// ahead of each API rather than once so that we can setup a custom
	// logger for each API call.
	setupAPIFunc := func(log logr.Logger, apiName string) (VClusterProvider, logr.Logger) {
		return &MockVClusterOps{}, logr.Logger{}
	}
	return mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
}

// mockVClusterOpsDispatchWithCustomSetup is like mockVClusterOpsDispatcher,
// except you provide your own setup API function.
func mockVClusterOpsDispatcherWithCustomSetup(vdb *vapi.VerticaDB,
	setupAPIFunc func(logr.Logger, string) (VClusterProvider, logr.Logger)) *VClusterOps {
	evWriter := aterrors.TestEVWriter{}
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

// VerifyNodeNameAddressMap is used in vcluster-ops unit test for verifying a map that contains correct
// node names and node addresses
func (m *MockVClusterOps) VerifyNodeNameAddressMap(nodeNameAddressMap map[string]string) error {
	if !reflect.DeepEqual(nodeNameAddressMap, TestNodeNameAddressMap) {
		return fmt.Errorf("failed to retrieve the map with node names and addresses")
	}
	return nil
}
