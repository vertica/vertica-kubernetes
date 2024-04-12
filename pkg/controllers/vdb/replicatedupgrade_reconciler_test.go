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
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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

	It("should sandbox subclusters in replica group B", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri2", Type: vapi.PrimarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(4))
		sbName := vmeta.GetReplicatedUpgradeSandbox(vdb.Annotations)
		Ω(sbName).Should(Equal(preferredSandboxName))
		pri1 := vdb.Spec.Subclusters[0]
		pri2 := vdb.Spec.Subclusters[1]
		Ω(pri1.Annotations).Should(HaveKey(vmeta.ChildSubclusterAnnotation))
		Ω(pri2.Annotations).Should(HaveKey(vmeta.ChildSubclusterAnnotation))

		sbMap := genSandboxMap(vdb)
		sbScs, found := sbMap[sbName]
		Ω(found).Should(BeTrue())
		Ω(sbScs).Should(HaveKey(pri1.Annotations[vmeta.ChildSubclusterAnnotation]))
		Ω(sbScs).Should(HaveKey(pri2.Annotations[vmeta.ChildSubclusterAnnotation]))

		// Should clear annotation at end of upgrade
		Ω(rr.Manager.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vmeta.GetReplicatedUpgradeSandbox(vdb.Annotations)).Should(Equal(""))
	})

	It("should handle collisions with the sandbox name", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "sec1", Type: vapi.SecondarySubcluster, Size: 2},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: preferredSandboxName, Subclusters: []vapi.SubclusterName{{Name: "sec1"}}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(3))
		sbName := vmeta.GetReplicatedUpgradeSandbox(vdb.Annotations)
		Ω(sbName).ShouldNot(Equal(preferredSandboxName))
		sbMap := genSandboxMap(vdb)
		Ω(sbMap).Should(HaveKey(sbName))
		Ω(sbMap).Should(HaveKey(preferredSandboxName))

		pri1 := vdb.Spec.Subclusters[0]
		Ω(pri1.Annotations).Should(HaveKey(vmeta.ChildSubclusterAnnotation))
		repSb := sbMap[sbName]
		Ω(repSb).Should(HaveKey(pri1.Annotations[vmeta.ChildSubclusterAnnotation]))
		firstSb := sbMap[preferredSandboxName]
		Ω(firstSb).Should(HaveKey("sec1"))
	})

	It("should wait for sandbox to finish", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri2", Type: vapi.PrimarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Wait should requeue since we aren't finished the sandbox yet
		Ω(rr.waitForSandboxToFinish(ctx)).Should(Equal(ctrl.Result{Requeue: true}))

		// Mock completion of sandbox
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(4))
		sbMap := genSandboxMap(vdb)
		for sbName := range sbMap {
			sbs := vapi.SandboxStatus{
				Name: sbName,
			}
			for _, scName := range sbMap[sbName] {
				sbs.Subclusters = append(sbs.Subclusters, scName.Name)
			}
			vdb.Status.Sandboxes = append(vdb.Status.Sandboxes, sbs)
		}
		Ω(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		Ω(rr.waitForSandboxToFinish(ctx)).Should(Equal(ctrl.Result{}))
	})

	It("should upgrade the vertica version in the sandbox", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		oldImageName := vdb.Spec.Image
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Sandboxes).Should(HaveLen(1))
		Ω(vdb.Spec.Sandboxes[0].Image).Should(Equal(oldImageName))

		Ω(rr.upgradeSandbox(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Sandboxes).Should(HaveLen(1))
		Ω(vdb.Spec.Sandboxes[0].Image).Should(Equal(NewImageName))
	})

	It("should use VerticReplicator CR to handle replication", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createReplicatedUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.startReplicationToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		replicatorName := vmeta.GetReplicatedUpgradeReplicator(vdb.Annotations)
		vrep := v1beta1.VerticaReplicator{}
		vrepNm := types.NamespacedName{
			Name:      replicatorName,
			Namespace: vdb.Namespace,
		}
		Ω(k8sClient.Get(ctx, vrepNm, &vrep)).Should(Succeed())

		Ω(vrep.Spec.Source.VerticaDB).Should(Equal(vdb.Name))
		Ω(vrep.Spec.Target.VerticaDB).Should(Equal(vdb.Name))
		Ω(vrep.Spec.Target.SandboxName).Should(Equal(vmeta.GetReplicatedUpgradeSandbox(vdb.Annotations)))

		Ω(rr.waitForReplicateToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{Requeue: true}))

		// Mock completion of replicaton
		meta.SetStatusCondition(&vrep.Status.Conditions,
			*vapi.MakeCondition(v1beta1.ReplicationComplete, metav1.ConditionTrue, "Done"))
		Ω(k8sClient.Status().Update(ctx, &vrep)).Should(Succeed())

		Ω(rr.waitForReplicateToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Verify VerticaReplicator was deleted
		Ω(k8sClient.Get(ctx, vrepNm, &vrep)).ShouldNot(Succeed())

		// Another attempt through waiting for replicator should not fail
		Ω(rr.waitForReplicateToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Annotations should be cleared when we finish the upgrade
		Ω(rr.Manager.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vmeta.GetReplicatedUpgradeReplicator(vdb.Annotations)).Should(Equal(""))
	})
})

func createReplicatedUpgradeReconciler(ctx context.Context, vdb *vapi.VerticaDB) *ReplicatedUpgradeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := createPodFactsDefault(fpr)
	dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
	actor := MakeReplicatedUpgradeReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
	r := actor.(*ReplicatedUpgradeReconciler)
	Ω(r.loadUpgradeState(ctx)).Should(Equal(ctrl.Result{}))
	return r
}

// SandboxMap will map the sandbox name to map of subcluster in that sandbox.
type sandboxMap = map[string]map[string]*vapi.Subcluster

// GenSandboxMap returns a map of sandboxes to a map of subclusters. This allows
// you to get all of the subclusters for a sandbox and give you quick access to
// each of the subclusters.
func genSandboxMap(vdb *vapi.VerticaDB) sandboxMap {
	if len(vdb.Spec.Sandboxes) == 0 {
		return nil
	}
	scMap := vdb.GenSubclusterMap()
	sbMap := sandboxMap{}
	for i := range vdb.Spec.Sandboxes {
		sb := vdb.Spec.Sandboxes[i].Name
		sbMap[sb] = make(map[string]*vapi.Subcluster)
		for j := range vdb.Spec.Sandboxes[i].Subclusters {
			if sc, found := scMap[vdb.Spec.Sandboxes[i].Subclusters[j].Name]; found {
				sbMap[sb][sc.Name] = sc
			}
		}
	}
	return sbMap
}
