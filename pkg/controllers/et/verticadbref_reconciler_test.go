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

package et

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("createet_reconciler", func() {
	ctx := context.Background()

	It("should continue on objects types other than VerticaDB", func() {
		et := vapi.MakeET()
		et.Spec.References[0].Object.Kind = "Pod"

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should continue on objects types other than VerticaDB APIVersion", func() {
		et := vapi.MakeET()
		et.Spec.References[0].Object.APIVersion = "version"

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should continue when no VerticaDB is running", func() {
		et := vapi.MakeET()

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should error when VerticaDB condition type doesn't exist", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		et := vapi.MakeET()
		et.Spec.Matches[0].Condition.Type = "NotWorking"

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		_, err := etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})
		Expect(err).ShouldNot(Succeed())
	})
})
