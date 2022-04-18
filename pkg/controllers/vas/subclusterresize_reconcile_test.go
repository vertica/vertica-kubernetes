/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("subclusterresize_reconcile", func() {
	ctx := context.Background()

	It("should requeue if VerticaDB doesn't exist", func() {
		vas := vapi.MakeVAS()
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should requeue if no subcluster exists with service name", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ServiceName = "not-there"
		vas.Spec.TargetSize = 5
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should resize subcluster if targetSize is set in vas", func() {
		const ScName = "sc1"
		var TargetSize int32 = 20
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, Size: 1},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.TargetSize = TargetSize
		vas.Spec.ServiceName = ScName
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(TargetSize))
	})

	It("should be a no-op if the targetSize isn't set", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.TargetSize = 0
		vas.Spec.ServiceName = vdb.Spec.Subclusters[0].GetServiceName()
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
	})

	It("should be a no-op if the targetSize matches actual", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.TargetSize = vdb.Spec.Subclusters[0].Size
		vas.Spec.ServiceName = vdb.Spec.Subclusters[0].GetServiceName()
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
	})

	It("should only grow the last subcluster defined", func() {
		const TargetSvcName = "conn"
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 5, ServiceName: TargetSvcName},
			{Name: "sc2", Size: 1, ServiceName: TargetSvcName},
			{Name: "sc3", Size: 10, ServiceName: "other"},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		const NumPodsToAdd = 5
		vas.Spec.TargetSize = vdb.Spec.Subclusters[0].Size + vdb.Spec.Subclusters[1].Size + NumPodsToAdd
		vas.Spec.ServiceName = TargetSvcName
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vas.Spec.TargetSize - vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[2].Size).Should(Equal(vdb.Spec.Subclusters[2].Size))
	})

	It("should shrink the subcluster size", func() {
		const TargetSvcName = "conn"
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 5, ServiceName: TargetSvcName},
			{Name: "sc2", Size: 10, ServiceName: "other"},
			{Name: "sc3", Size: 1, ServiceName: TargetSvcName},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		const NumPodsToRemove = 3
		vas.Spec.TargetSize = vdb.Spec.Subclusters[0].Size + vdb.Spec.Subclusters[2].Size - NumPodsToRemove
		vas.Spec.ServiceName = TargetSvcName
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size + vdb.Spec.Subclusters[2].Size - NumPodsToRemove))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vdb.Spec.Subclusters[1].Size))
		Expect(fetchVdb.Spec.Subclusters[2].Size).Should(Equal(int32(0)))
	})
})
