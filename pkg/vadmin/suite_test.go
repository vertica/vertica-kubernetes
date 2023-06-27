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
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var logger logr.Logger

var _ = BeforeSuite(func() {
	logger = zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true))
})

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "vadmin Suite")
}

// mockAdmintoolsDispatcher will create an admintools dispatcher for test
// purposes.
func mockAdmintoolsDispatcher() (Admintools, *vapi.VerticaDB, *cmds.FakePodRunner) {
	vdb := vapi.MakeVDB()
	fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
	evWriter := aterrors.TestEVWriter{}
	dispatcher := MakeAdmintools(logger, vdb, fpr, &evWriter, false)
	return dispatcher.(Admintools), vdb, fpr
}
