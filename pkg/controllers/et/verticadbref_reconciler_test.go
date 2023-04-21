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
	"github.com/vertica/vertica-kubernetes/pkg/etstatus"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("createet_reconciler", func() {
	ctx := context.Background()

	It("should succeed with no-op when no VerticaDB is running", func() {
		et := vapi.MakeET()

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
	})

	It("should succeed with no-op when VerticaDB condition type doesn't exist", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		et := vapi.MakeET()
		et.Spec.Matches[0].Condition.Type = "NotWorking"

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))
	})

	It("should succeed with no-op when reference status job exists already exists", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		et := vapi.MakeET()
		status := vapi.ETRefObjectStatus{
			Namespace: et.Spec.References[0].Object.Namespace,
			Name:      et.Spec.References[0].Object.Name,
			Kind:      et.Spec.References[0].Object.Kind,
			JobName:   "test",
		}

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()
		Expect(etstatus.Apply(ctx, k8sClient, et, &status)).Should(Succeed())
		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
	})

	It("should succeed with no-op when creating the job", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		et := vapi.MakeET()
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()
		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
	})
})

func getEventTriggerStatus(ctx context.Context, nm types.NamespacedName) vapi.EventTrigger {
	etrigger := vapi.EventTrigger{}
	etStatus := k8sClient.Get(ctx, nm, &etrigger)
	Expect(etStatus).Should(Succeed())

	return etrigger
}
