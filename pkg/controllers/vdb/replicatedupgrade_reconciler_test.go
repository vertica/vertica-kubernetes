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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	v1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("replicatedupgrade_reconciler", func() {
	ctx := context.Background()
	const NewImageName = "different-image"

	It("should correctly assign replica groups for both subcluster types", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 6},
			{Name: "sc2", Type: vapi.SecondarySubcluster, Size: 3},
			{Name: "sc3", Type: vapi.SecondarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		replicatedReconiler := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(replicatedReconiler.assignSubclustersToReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))
		Ω(vdb.Spec.Subclusters[0].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
		Ω(vdb.Spec.Subclusters[1].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
		Ω(vdb.Spec.Subclusters[2].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
	})

	It("should create new secondaries for each of the primaries", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 6, ServiceType: v1.ServiceTypeLoadBalancer},
			{Name: "sc2", Type: vapi.SecondarySubcluster, Size: 3},
			{Name: "sc3", Type: vapi.PrimarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(5))
		sc1 := vdb.Spec.Subclusters[0]
		sc3 := vdb.Spec.Subclusters[3]
		Ω(sc3.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc3.Name).Should(HavePrefix("sc1-"))
		Ω(sc3.ServiceType).Should(Equal(v1.ServiceTypeClusterIP))
		Ω(sc3.Size).Should(Equal(int32(6)))
		Ω(sc3.Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupBValue))
		Ω(sc3.Annotations).Should(HaveKeyWithValue(vmeta.ParentSubclusterAnnotation, sc1.Name))
		Ω(sc1.Annotations).Should(HaveKeyWithValue(vmeta.ChildSubclusterAnnotation, sc3.Name))

		sc4 := vdb.Spec.Subclusters[4]
		Ω(sc4.Name).Should(HavePrefix("sc3-"))
		Ω(sc4.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc4.Size).Should(Equal(int32(2)))
		Ω(sc4.Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupBValue))
	})

	It("should generate unique subcluster name on collision during scale out", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 6},
			{Name: "sc1-sb", Type: vapi.SecondarySubcluster, Size: 3},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(3))
		sc3 := vdb.Spec.Subclusters[2]
		Ω(sc3.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc3.Name).Should(HavePrefix("sc1-"))
		Ω(sc3.Name).ShouldNot(Equal("sc1-sb"))
	})
})

func createReplicatedUpgradeReconciler(ctx context.Context, vdb *vapi.VerticaDB) *ReplicatedUpgradeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := createPodFactsDefault(fpr)
	dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
	actor := MakeReplicatedUpgradeReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
	r := actor.(*ReplicatedUpgradeReconciler)
	Ω(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
	return r
}
