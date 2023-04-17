/*
Copyright [2021-2023] Micro Focus or one of its affiliates.

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

package et

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("createet_reconciler", func() {
	ctx := context.Background()

	It("should reconcile an EventTrigger with no errors if reference object doesn't exist", func() {
		vdb := vapi.MakeVDB() // Intentionally not creating it as we want the reconcile to be a no-op
		et := vapi.MakeET()
		et.Spec.References[0] = *makeETRefObjectOfVDB(vdb)
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should reconcile an EventTrigger with no errors if ET doesn't exist", func() {
		et := vapi.MakeET()
		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})
})
