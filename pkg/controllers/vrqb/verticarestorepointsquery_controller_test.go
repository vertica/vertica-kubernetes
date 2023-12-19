/*
Copyright [2021-2023] Open Text.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package vrqb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verticarestorepointsquery_controller", func() {
	ctx := context.Background()

	It("should reconcile an Vertica Restore Points Query with no errors if vrqb doesn't exist", func() {
		vrqb := vapi.MakeVrqb()
		Expect(vrqbRec.Reconcile(ctx, ctrl.Request{NamespacedName: vrqb.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should suspend the reconcile if pause annotation is set", func() {
		vrqb := vapi.MakeVrqb()
		vrqb.Annotations = map[string]string{meta.PauseOperatorAnnotation: "1"}
		Expect(k8sClient.Create(ctx, vrqb)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrqb)).Should(Succeed()) }()
		Expect(vrqbRec.Reconcile(ctx, ctrl.Request{NamespacedName: vrqb.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})
})
