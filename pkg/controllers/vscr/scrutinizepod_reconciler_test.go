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
	test "github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("scrutinizepod_reconciler", func() {
	ctx := context.Background()

	It("should create scrutinize pod", func() {
		vdb := v1beta1.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeScrutinizePodReconciler(vscrRec, vscr, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, vscr.ExtractNamespacedName(), pod)).Should(Succeed())
	})

	It("should exit early without error if ScrutinizePodCreated is true", func() {
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)
		cond := v1.MakeCondition(v1beta1.ScrutinizePodCreated, metav1.ConditionTrue, "")
		meta.SetStatusCondition(&vscr.Status.Conditions, *cond)

		r := MakeScrutinizePodReconciler(vscrRec, vscr, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
	})

	It("should requeue if vdb does not exist", func() {
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeScrutinizePodReconciler(vscrRec, vscr, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	})
})
