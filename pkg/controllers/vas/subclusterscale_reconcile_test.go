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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("subclusterscale_reconcile", func() {
	ctx := context.Background()

	It("should grow by adding new subclusters", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ScalingGranularity = vapi.SubclusterScalingGranularity
		vas.Spec.Template = vapi.Subcluster{
			Name:        "blah",
			ServiceName: "my-ut",
			Size:        8,
		}
		vas.Spec.TargetSize = vas.Spec.Template.Size * 2
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		nm := vapi.MakeVDBName()
		Expect(k8sClient.Get(ctx, nm, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(3))
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vas.Spec.Template.Size))
		Expect(fetchVdb.Spec.Subclusters[1].Name).Should(Equal("blah-0"))
		Expect(fetchVdb.Spec.Subclusters[2].Size).Should(Equal(vas.Spec.Template.Size))
		Expect(fetchVdb.Spec.Subclusters[2].Name).Should(Equal("blah-1"))
	})

	It("should shrink only when delta from targetPod is an entire subcluster", func() {
		vdb := vapi.MakeVDB()
		const ServiceName = "as"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 5, ServiceName: ServiceName},
			{Name: "sc2", Size: 20, ServiceName: "pri"},
			{Name: "sc3a", Size: 1, ServiceName: ServiceName},
			{Name: "sc3b", Size: 9, ServiceName: ServiceName},
			{Name: "sc4", Size: 7, ServiceName: "other-svc"},
			{Name: "sc5", Size: 2, ServiceName: ServiceName},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ScalingGranularity = vapi.SubclusterScalingGranularity
		vas.Spec.ServiceName = ServiceName
		vas.Spec.Template = vapi.Subcluster{
			Name:        "blah",
			ServiceName: ServiceName,
			Size:        5,
		}
		vas.Spec.TargetSize = 8
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		vdbName := vdb.ExtractNamespacedName()
		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(5))
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vdb.Spec.Subclusters[1].Size))
		Expect(fetchVdb.Spec.Subclusters[2].Size).Should(Equal(vdb.Spec.Subclusters[2].Size))
		Expect(fetchVdb.Spec.Subclusters[3].Size).Should(Equal(vdb.Spec.Subclusters[3].Size))
		Expect(fetchVdb.Spec.Subclusters[4].Size).Should(Equal(vdb.Spec.Subclusters[4].Size))

		vasName := vapi.MakeVASName()
		Expect(k8sClient.Get(ctx, vasName, vas)).Should(Succeed())
		vas.Spec.TargetSize = 3
		Expect(k8sClient.Update(ctx, vas)).Should(Succeed())
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(3))
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vdb.Spec.Subclusters[1].Size))
		Expect(fetchVdb.Spec.Subclusters[2].Size).Should(Equal(vdb.Spec.Subclusters[4].Size))
	})

	It("should get rid of all subclusters if shrinking to zero is allowed", func() {
		vdb := vapi.MakeVDB()
		const ServiceName = "as"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 5, ServiceName: ServiceName},
			{Name: "sc2", Size: 20, ServiceName: "pri"},
			{Name: "sc3", Size: 9, ServiceName: ServiceName},
			{Name: "sc4", Size: 7, ServiceName: "pri"},
			{Name: "sc5", Size: 3, ServiceName: ServiceName},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ScalingGranularity = vapi.SubclusterScalingGranularity
		vas.Spec.ServiceName = ServiceName
		vas.Spec.Template = vapi.Subcluster{
			Name:        "blah",
			ServiceName: ServiceName,
			Size:        5,
		}
		vas.Spec.TargetSize = 0
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		vdbName := vdb.ExtractNamespacedName()
		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		// Expect no change since targetSize is zero without allowScaleToZero
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(5))

		vasName := vapi.MakeVASName()
		Expect(k8sClient.Get(ctx, vasName, vas)).Should(Succeed())
		vas.Spec.TargetSize = 0
		Expect(k8sClient.Update(ctx, vas)).Should(Succeed())

		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(2))
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[1].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vdb.Spec.Subclusters[3].Size))
	})

	It("should use an existing subcluster as base if scaling out", func() {
		vdb := vapi.MakeVDB()
		const ServiceName = "as"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "base", Size: 5, ServiceName: ServiceName},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ScalingGranularity = vapi.SubclusterScalingGranularity
		vas.Spec.ServiceName = ServiceName
		vas.Spec.Template.Size = 0
		vas.Spec.TargetSize = 8
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		vdbName := vdb.ExtractNamespacedName()
		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(1))

		vasName := vapi.MakeVASName()
		Expect(k8sClient.Get(ctx, vasName, vas)).Should(Succeed())
		vas.Spec.TargetSize = 13
		Expect(k8sClient.Update(ctx, vas)).Should(Succeed())

		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(2))
		Expect(fetchVdb.Spec.Subclusters[0].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Size).Should(Equal(vdb.Spec.Subclusters[0].Size))
		Expect(fetchVdb.Spec.Subclusters[1].Name).Should(Equal(fmt.Sprintf("%s-0", vas.Name)))
		Expect(fetchVdb.Spec.Subclusters[1].ServiceName).Should(Equal(ServiceName))
	})

	It("should not scale out if no template or existing subcluster can be used", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vas := vapi.MakeVAS()
		vas.Spec.ScalingGranularity = vapi.SubclusterScalingGranularity
		vas.Spec.ServiceName = "BrandNewServiceName"
		vas.Spec.Template.Size = 0
		vas.Spec.TargetSize = 50
		test.CreateVAS(ctx, k8sClient, vas)
		defer test.DeleteVAS(ctx, k8sClient, vas)

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		Expect(vasRec.Reconcile(ctx, req)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		vdbName := vdb.ExtractNamespacedName()
		Expect(k8sClient.Get(ctx, vdbName, fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Spec.Subclusters)).Should(Equal(1))
	})
})
