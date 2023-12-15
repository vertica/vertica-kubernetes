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

package cloud

import (
	"errors"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

func TestCloud(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "cloud Suite")
}

type testEVWriter struct{}

func (t *testEVWriter) Event(_ runtime.Object, _, _, _ string)                    {}
func (t *testEVWriter) Eventf(_ runtime.Object, _, _, _ string, _ ...interface{}) {}

var _ = Describe("cloud/secret", func() {
	It("should requeue if returned a not found error", func() {
		cf := ControllerSecretFetcher{
			EVWriter: &testEVWriter{},
		}
		nfe := secrets.NotFoundError{}
		errs := errors.Join(errors.New("error 1"), &nfe)
		secretData, res, err := cf.handleFetchError(types.NamespacedName{Name: "secret"}, errs)
		Ω(secretData).Should(BeNil())
		Ω(res).Should(Equal(ctrl.Result{Requeue: true}))
		Ω(err).Should(BeNil())
	})

	It("should not requeue if some other error is returned", func() {
		cf := ControllerSecretFetcher{
			EVWriter: &testEVWriter{},
		}
		secretData, res, err := cf.handleFetchError(types.NamespacedName{Name: "secret"}, errors.New("panic"))
		Ω(secretData).Should(BeNil())
		Ω(res).Should(Equal(ctrl.Result{}))
		Ω(err).ShouldNot(BeNil())
	})
})
