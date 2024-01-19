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

package vrpq

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	test "github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("query_reconcile", func() {
	ctx := context.Background()

	It("should requeue if VerticaDB doesn't exist", func() {
		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		req := ctrl.Request{NamespacedName: vapi.MakeSampleVrpqName()}
		Expect(vrpqRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should update query conditions if the vclusterops API succeeded", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := vapi.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		recon := MakeRestorePointsQueryReconciler(vrpqRec, vrpq, logger)

		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// make sure that Quering condition is updated to false and
		// QueryComplete condition is updated to True
		Expect(vrpq.IsStatusConditionFalse(vapi.Querying)).Should(BeTrue())
		Expect(vrpq.IsStatusConditionTrue(vapi.QueryComplete)).Should(BeTrue())

	})
})
