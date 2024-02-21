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

package vrpq

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"

	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("vdbverify_reconcile", func() {
	ctx := context.Background()

	It("should requeue if VerticaDB doesn't exist", func() {
		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()

		req := ctrl.Request{NamespacedName: v1beta1.MakeSampleVrpqName()}
		Expect(vrpqRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should update the queryReady condition and state to false with admintools", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = "v24.2.0"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		recon := MakeVDBVerifyReconciler(vrpqRec, vrpq, logger)
		result, _ := recon.Reconcile(ctx, &ctrl.Request{})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(vrpq.Status.Conditions[0].Reason).Should(Equal("AdmintoolsNotSupported"))

		// QueryReady condition is updated to False
		Expect(vrpq.IsStatusConditionFalse(v1beta1.QueryReady)).Should(BeTrue())
		Expect(vrpq.Status.State).Should(Equal(stateIncompatibleDB))
	})

	It("should update the queryReady condition and state to false for incompatible databases", func() {
		vdb := vapi.MakeVDB()
		secretName := "tls-2"
		vdb.Spec.NMATLSSecret = secretName
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vrpq := v1beta1.MakeVrpq()
		Expect(k8sClient.Create(ctx, vrpq)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrpq)).Should(Succeed()) }()
		recon := MakeVDBVerifyReconciler(vrpqRec, vrpq, logger)
		result, _ := recon.Reconcile(ctx, &ctrl.Request{})
		Expect(result).Should(Equal(ctrl.Result{}))
		Expect(vrpq.Status.Conditions[0].Reason).Should(Equal("IncompatibleDB"))

		// QueryReady condition is updated to False
		Expect(vrpq.IsStatusConditionFalse(v1beta1.QueryReady)).Should(BeTrue())
		Expect(vrpq.Status.State).Should(Equal(stateIncompatibleDB))
	})
})
