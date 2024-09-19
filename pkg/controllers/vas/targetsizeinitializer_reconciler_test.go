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

package vas

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	test "github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("targetsizeinitializer_reconcile", func() {
	ctx := context.Background()

	It("should init the targetsize for a new vas", func() {
		const ServiceName = "as"
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 10, ServiceName: ServiceName},
			{Name: "sc2", Size: 15, ServiceName: ServiceName},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := v1beta1.MakeVAS()
		vas.Spec.ServiceName = ServiceName
		vas.Spec.TargetSize = 0
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: v1beta1.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVas := &v1beta1.VerticaAutoscaler{}
		nm := v1beta1.MakeVASName()
		Expect(k8sClient.Get(ctx, nm, fetchVas)).Should(Succeed())
		Expect(fetchVas.Spec.TargetSize).Should(Equal(int32(25)))
		Expect(len(fetchVas.Status.Conditions)).Should(Equal(1))
		Expect(fetchVas.Status.Conditions[v1beta1.TargetSizeInitializedIndex].Status).Should(Equal(corev1.ConditionTrue))
	})
})
