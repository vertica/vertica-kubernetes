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

func testIncompatibleDB(ctx context.Context, sourceVersion, targetVersion string,
	sourceUsingVclusteropsDeployment bool, expectedReason string,
	expectedStatusConditionValue bool, expectedState string) {
	sourceVdbName := v1beta1.MakeSourceVDBName()
	sourceVdb := vapi.MakeVDB()
	sourceVdb.Name = sourceVdbName.Name
	sourceVdb.Namespace = sourceVdbName.Namespace
	if sourceUsingVclusteropsDeployment {
		sourceVdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
	}
	sourceVdb.Annotations[vmeta.VersionAnnotation] = sourceVersion
	test.CreateVDB(ctx, k8sClient, sourceVdb)
	defer test.DeleteVDB(ctx, k8sClient, sourceVdb)

	targetVdbName := v1beta1.MakeTargetVDBName()
	targetVdb := vapi.MakeVDB()
	targetVdb.Name = targetVdbName.Name
	targetVdb.Namespace = targetVdbName.Namespace
	targetVdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
	targetVdb.Annotations[vmeta.VersionAnnotation] = targetVersion
	targetVdb.UID = testTargetVdbUID
	test.CreateVDB(ctx, k8sClient, targetVdb)
	defer test.DeleteVDB(ctx, k8sClient, targetVdb)

	vrep := v1beta1.MakeVrep()
	Expect(k8sClient.Create(ctx, vrep)).Should(Succeed())
	defer func() { Expect(k8sClient.Delete(ctx, vrep)).Should(Succeed()) }()
	recon := MakeVdbVerifyReconciler(vrepRec, vrep, logger)
	result, err := recon.Reconcile(ctx, &ctrl.Request{})
	Expect(result).Should(Equal(ctrl.Result{}))
	Expect(err).ShouldNot(HaveOccurred())
	Expect(vrep.Status.Conditions[0].Reason).Should(Equal(expectedReason))

	// ReplicationReady condition is updated to expectedStatusConditionValue
	Expect(vrep.IsStatusConditionTrue(v1beta1.ReplicationReady)).
		Should(Equal(expectedStatusConditionValue))
	Expect(vrep.Status.State).Should(Equal(expectedState))
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

	It("should update the ReplicationReady condition and state to false for incompatible source database version", func() {
		testIncompatibleDB(ctx, "v24.2.0", "v24.3.0", true, "IncompatibleSourceDB", false, stateIncompatibleDB)
	})

	It("should update the ReplicationReady condition and state to false for incompatible target database version", func() {
		testIncompatibleDB(ctx, "v24.4.0", "v24.3.0", true, "IncompatibleTargetDB", false, stateIncompatibleDB)
	})

	It("should update the ReplicationReady condition and state to false for incompatible source database deployment type", func() {
		testIncompatibleDB(ctx, "v24.3.0", "v24.3.0", false, "AdmintoolsNotSupported", false, stateIncompatibleDB)
	})

	It("should update the ReplicationReady condition and state to true for compatible source and target databases", func() {
		testIncompatibleDB(ctx, "v24.3.0", "v24.3.0", true, "Ready", true, "Ready")
	})

	It("should update the ReplicationReady condition and state to true for compatible source and target databases", func() {
		testIncompatibleDB(ctx, "v24.3.0", "v24.4.0", true, "Ready", true, "Ready")
	})
})
