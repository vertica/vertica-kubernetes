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

package vscr

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verticascrutinize_controller", func() {
	ctx := context.Background()

	It("should reconcile successfully if vscr is not found", func() {
		vscr := vapi.MakeVscr()
		Expect(vscrRec.Reconcile(ctx, ctrl.Request{NamespacedName: vscr.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should suspend the reconcile if pause annotation is set", func() {
		vscr := vapi.MakeVscr()
		vscr.Annotations = map[string]string{meta.PauseOperatorAnnotation: "1"}
		Expect(k8sClient.Create(ctx, vscr)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vscr)).Should(Succeed()) }()
		Expect(vscrRec.Reconcile(ctx, ctrl.Request{NamespacedName: vscr.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})
})
