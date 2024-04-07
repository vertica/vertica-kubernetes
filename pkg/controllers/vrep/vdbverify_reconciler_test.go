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

package vrep

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

func testIncompatibleDB(ctx context.Context, sourceVersion, targetVersion, expectedReason string) {
	sourceVDBName := v1beta1.MakeSourceVDBName()
	sourceVDB := vapi.MakeVDB()
	sourceVDB.Name = sourceVDBName.Name
	sourceVDB.Namespace = sourceVDBName.Namespace
	sourceVDB.Annotations[vmeta.VersionAnnotation] = sourceVersion
	test.CreateVDB(ctx, k8sClient, sourceVDB)
	defer test.DeleteVDB(ctx, k8sClient, sourceVDB)

	targetVDBName := v1beta1.MakeTargetVDBName()
	targetVDB := vapi.MakeVDB()
	targetVDB.Name = targetVDBName.Name
	targetVDB.Namespace = targetVDBName.Namespace
	targetVDB.Annotations[vmeta.VersionAnnotation] = targetVersion
	test.CreateVDB(ctx, k8sClient, targetVDB)
	defer test.DeleteVDB(ctx, k8sClient, targetVDB)

	vrep := v1beta1.MakeVrep()
	Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
	defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()
	recon := MakeVdbVerifyReconciler(vrepRec, vrep, logger)
	result, err := recon.Reconcile(ctx, &ctrl.Request{})
	Expect(result).Should(Equal(ctrl.Result{}))
	Expect(err).ShouldNot(HaveOccurred())
	Expect(vrep.Status.Conditions[0].Reason).Should(Equal(expectedReason))

	// ReplicationReady condition is updated to False
	Expect(vrep.IsStatusConditionFalse(v1beta1.ReplicationReady)).Should(BeTrue())
	Expect(vrep.Status.State).Should(Equal(stateIncompatibleDB))
}

var _ = Describe("vdbverify_reconcile", func() {
	ctx := context.Background()

	It("should requeue if VerticaDB doesn't exist", func() {
		vrep := v1beta1.MakeVrep()
		Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
		defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()

		req := ctrl.Request{NamespacedName: v1beta1.MakeSampleVrepName()}
		Expect(vrepRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should update the ReplicationReady condition and state to false for incompatible source database", func() {
		testIncompatibleDB(ctx, "v24.2.0", "v24.3.0", "IncompatibleSourceDB")
	})

	It("should update the ReplicationReady condition and state to false for incompatible target database", func() {
		testIncompatibleDB(ctx, "v24.3.0", "v24.2.0", "IncompatibleTargetDB")
	})
})
