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
	v1vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/etstatus"
	test "github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
	})

	It("should succeed with no-op when VerticaDB condition type not match", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		cond := []metav1.Condition{
			{Type: v1vapi.AutoRestartVertica, Status: metav1.ConditionTrue, Reason: v1vapi.Unknown},
			{Type: v1vapi.DBInitialized, Status: metav1.ConditionTrue, Reason: v1vapi.Unknown},
		}
		Expect(setVerticaStatus(ctx, k8sClient, vdb, cond)).Should(Succeed())

		et := vapi.MakeET()
		et.Spec.Matches[0].Condition.Type = string(vapi.VerticaRestartNeeded)

		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()

		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
		Expect(etrigger.Status.References[0].JobName).Should(BeEmpty())
		Expect(etrigger.Status.References[0].JobNamespace).Should(BeEmpty())
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
		Expect(etstatus.Apply(ctx, k8sClient, logger, et, &status)).Should(Succeed())
		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: et.ExtractNamespacedName()})).Should(Equal(ctrl.Result{}))

		nm := et.ExtractNamespacedName()
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
	})

	It("should succeed with no-op when creating the job", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		cond := []metav1.Condition{
			{Type: v1vapi.AutoRestartVertica, Status: metav1.ConditionTrue, Reason: v1vapi.Unknown},
			{Type: v1vapi.DBInitialized, Status: metav1.ConditionTrue, Reason: v1vapi.Unknown},
		}
		Expect(setVerticaStatus(ctx, k8sClient, vdb, cond)).Should(Succeed())

		et := vapi.MakeET()
		nm := et.ExtractNamespacedName()
		Expect(k8sClient.Create(ctx, et)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, et)).Should(Succeed()) }()
		Expect(etRec.Reconcile(ctx, ctrl.Request{NamespacedName: nm})).Should(Equal(ctrl.Result{}))

		job := makeJob(et)
		etrigger := getEventTriggerStatus(ctx, nm)
		Expect(etrigger.Status.References).ShouldNot(BeNil())
		Expect(etrigger.Status.References[0].JobName).Should(Equal(et.Spec.Template.Metadata.Name))
		Expect(etrigger.Status.References[0].JobNamespace).Should(Equal(et.Namespace))
		defer func() { Expect(k8sClient.Delete(ctx, job)).Should(Succeed()) }()
	})
})

func getEventTriggerStatus(ctx context.Context, nm types.NamespacedName) vapi.EventTrigger {
	etrigger := vapi.EventTrigger{}
	etStatus := k8sClient.Get(ctx, nm, &etrigger)
	Expect(etStatus).Should(Succeed())

	return etrigger
}

func setVerticaStatus(ctx context.Context, clnt client.Client, vdb *vapi.VerticaDB, conditions []metav1.Condition) error {
	v1vdb := v1vapi.VerticaDB{}
	err := vdb.ConvertTo(&v1vdb)
	Expect(err).Should(Succeed())

	for idx := range conditions {
		if err := vdbstatus.UpdateCondition(ctx, clnt, &v1vdb, &conditions[idx]); err != nil {
			return err
		}
	}

	return nil
}
