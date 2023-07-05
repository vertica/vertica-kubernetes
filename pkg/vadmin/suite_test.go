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
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
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

const TestPassword = "test-pw"
const TestParm = "Parm1"
const TestValue = "val1"

// mockVClusterOpsDispatcher will create an vcluster-ops dispatcher for test purposes
func mockVClusterOpsDispatcher() *VClusterOps {
	vdb := vapi.MakeVDBForHTTP("test-secret")
	mockVops := MockVClusterOps{}
	dispatcher := MakeVClusterOps(logger, vdb, k8sClient, &mockVops, TestPassword)
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
