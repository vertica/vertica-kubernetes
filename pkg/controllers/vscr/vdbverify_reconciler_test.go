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

package vscr

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("scrutinizepod_reconciler", func() {
	ctx := context.Background()

	It("should reconcile successfully", func() {
		vdb := v1.MakeVDBForVclusterOps()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)

		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeFalse())
		runVDBVerifyReconcile(ctx, vscr)
		checkStatusConditionAfterReconcile(ctx, vscr, metav1.ConditionTrue, events.VerticaDBSetForScrutinize)
	})

	It("should update status if vclusterops is disabled", func() {
		vdb := v1.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)

		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeFalse())
		runVDBVerifyReconcile(ctx, vscr)
		checkStatusConditionAfterReconcile(ctx, vscr, metav1.ConditionFalse, events.VclusterOpsDisabled)
	})

	It("should update status if server version does not have scrutinize support through vclusterOps", func() {
		vdb := v1.MakeVDBForVclusterOps()
		vdb.Annotations[vmeta.VersionAnnotation] = "v23.4.0"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)

		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeFalse())
		runVDBVerifyReconcile(ctx, vscr)
		checkStatusConditionAfterReconcile(ctx, vscr, metav1.ConditionFalse, events.VclusterOpsScrutinizeNotSupported)
	})

	It("should update status if vdb does not have server version info", func() {
		vdb := v1.MakeVDBForVclusterOps()
		delete(vdb.Annotations, vmeta.VersionAnnotation)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)

		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeFalse())
		runVDBVerifyReconcile(ctx, vscr)
		checkStatusConditionAfterReconcile(ctx, vscr, metav1.ConditionFalse, events.VerticaVersionNotFound)

	})

	It("should update status if vdb does not exist", func() {
		vscr := v1beta1.MakeVscr()
		v1beta1_test.CreateVSCR(ctx, k8sClient, vscr)
		defer v1beta1_test.DeleteVSCR(ctx, k8sClient, vscr)

		Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeFalse())
		runVDBVerifyReconcile(ctx, vscr)
		checkStatusConditionAfterReconcile(ctx, vscr, metav1.ConditionFalse, events.VerticaDBNotFound)
	})
})

func checkStatusConditionAfterReconcile(ctx context.Context, vscr *v1beta1.VerticaScrutinize,
	status metav1.ConditionStatus, reason string) {
	Expect(k8sClient.Get(ctx, vscr.ExtractNamespacedName(), vscr)).Should(Succeed())
	Expect(vscr.IsStatusConditionPresent(v1beta1.ScrutinizeReady)).Should(BeTrue())
	Expect(vscr.Status.Conditions[0]).Should(test.EqualMetaV1Condition(
		metav1.Condition{
			Type:   v1beta1.ScrutinizeReady,
			Status: status,
			Reason: reason,
		},
	))
}

func runVDBVerifyReconcile(ctx context.Context, vscr *v1beta1.VerticaScrutinize) {
	r := MakeVDBVerifyReconciler(vscrRec, vscr, logger)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(err).Should(Succeed())
	Expect(res).Should(Equal(ctrl.Result{}))
}
