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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("saverestorepoint_reconciler", func() {
	ctx := context.Background()

	It("should update status if vclusterops is disabled", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		meta.SetStatusCondition(&vdb.Status.Conditions,
			*vapi.MakeCondition(vapi.SaveRestorePointNeeded, metav1.ConditionTrue, "Done"))
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		fpr := &cmds.FakePodRunner{}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSaveRestorePointReconciler(vdbRec, vdb, logger, &PodFacts{}, dispatcher)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.IsStatusConditionFalse(vapi.SaveRestorePointNeeded)).Should(BeTrue())
	})
})
