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
	"fmt"

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/aterrors"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/mockvops"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("onlineupgrade_reconciler", func() {
	ctx := context.Background()
	const NewImageName = "different-image"

	It("should correctly assign replica groups for both subcluster types", func() {
		vdb := vapi.MakeVDBForVclusterOps()
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

		onlineReconiler := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(onlineReconiler.assignSubclustersToReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))
		Ω(vdb.Spec.Subclusters[0].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
		Ω(vdb.Spec.Subclusters[1].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
		Ω(vdb.Spec.Subclusters[2].Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupAValue))
	})

	It("should create new secondaries for each of the primaries", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc2", Type: vapi.SecondarySubcluster, Size: 3},
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 6,
				ServiceType:         v1.ServiceTypeNodePort,
				ClientNodePort:      32001,
				VerticaHTTPNodePort: 32002,
			},
			{Name: "sc3", Type: vapi.PrimarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(6))
		sc1 := vdb.Spec.Subclusters[1]
		sc3 := vdb.Spec.Subclusters[3]
		Ω(sc3.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc3.Name).Should(HavePrefix("sc1-"))
		Ω(sc3.ServiceType).Should(Equal(v1.ServiceTypeNodePort))
		Ω(sc3.ClientNodePort).Should(Equal(int32(32001)))
		Ω(sc3.VerticaHTTPNodePort).Should(Equal(int32(32002)))
		Ω(sc3.Size).Should(Equal(int32(6)))
		Ω(sc3.Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupBValue))
		Ω(sc3.Annotations).Should(HaveKeyWithValue(vmeta.ParentSubclusterAnnotation, sc1.Name))
		Ω(sc1.Annotations).Should(HaveKeyWithValue(vmeta.ChildSubclusterAnnotation, sc3.Name))
		Ω(sc3.Annotations).Should(HaveKeyWithValue(vmeta.ParentSubclusterTypeAnnotation, vapi.PrimarySubcluster))

		sc5 := vdb.Spec.Subclusters[4]
		Ω(sc5.Name).Should(HavePrefix("sc3-"))
		Ω(sc5.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc5.Size).Should(Equal(int32(2)))
		Ω(sc5.Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupBValue))
		Ω(sc5.Annotations).Should(HaveKeyWithValue(vmeta.ParentSubclusterTypeAnnotation, vapi.PrimarySubcluster))

		sc4 := vdb.Spec.Subclusters[5]
		Ω(sc4.Name).Should(HavePrefix("sc2-"))
		Ω(sc4.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc4.Size).Should(Equal(int32(3)))
		Ω(sc4.Annotations).Should(HaveKeyWithValue(vmeta.ReplicaGroupAnnotation, vmeta.ReplicaGroupBValue))
		Ω(sc4.Annotations).Should(HaveKeyWithValue(vmeta.ParentSubclusterTypeAnnotation, vapi.SecondarySubcluster))

	})

	It("should generate unique subcluster name on collision during scale out", func() {
		vdb := vapi.MakeVDBForVclusterOps()
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

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(4))
		sc3 := vdb.Spec.Subclusters[2]
		sc4 := vdb.Spec.Subclusters[3]
		Ω(sc3.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc3.Name).Should(HavePrefix("sc1-"))
		Ω(sc3.Name).ShouldNot(Equal("sc1-sb"))
		Ω(sc4.Type).Should(Equal(vapi.SecondarySubcluster))
		Ω(sc4.Name).Should(HavePrefix("sc1-sb-"))
		Ω(sc4.Name).ShouldNot(Equal("sc1"))
	})

	It("should sandbox subclusters in replica group B", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Annotations[vmeta.OnlineUpgradeSandboxNameAnnotation] = preferredSandboxName
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

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(4))
		sbName := vmeta.GetOnlineUpgradeSandbox(vdb.Annotations)
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
		Ω(rr.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vmeta.GetOnlineUpgradeSandbox(vdb.Annotations)).Should(Equal(""))
	})

	It("should handle collisions with the sandbox name", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Annotations[vmeta.OnlineUpgradeSandboxNameAnnotation] = preferredSandboxName
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "sec1", Type: vapi.SecondarySubcluster, Size: 2},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: preferredSandboxName, Subclusters: []vapi.SubclusterName{{Name: "sec1"}}},
		}
		vdb.Spec.NMATLSSecret = "test-tls"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, k8sClient, vdb.Spec.NMATLSSecret)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(3))
		sbName := vmeta.GetOnlineUpgradeSandbox(vdb.Annotations)
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

	It("should treat sandbox as a no-op if already done", func() {
		vdb := vapi.MakeVDBForVclusterOps()
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

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Mock completion of sandbox
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(4))
		mockCompletionOfSandbox(ctx, vdb)

		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
	})

	It("should upgrade the vertica version in the sandbox", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		oldImageName := vdb.Spec.Image
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		mockCompletionOfSandbox(ctx, vdb)

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Sandboxes).Should(HaveLen(1))
		Ω(vdb.Spec.Sandboxes[0].Image).Should(Equal(oldImageName))

		Ω(rr.upgradeSandbox(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Sandboxes).Should(HaveLen(1))
		Ω(vdb.Spec.Sandboxes[0].Image).Should(Equal(NewImageName))
	})

	It("should use VerticaReplicator CR to handle replication", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.startReplicationToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		replicatorName := vmeta.GetOnlineUpgradeReplicator(vdb.Annotations)
		vrep := v1beta1.VerticaReplicator{}
		vrepNm := types.NamespacedName{
			Name:      replicatorName,
			Namespace: vdb.Namespace,
		}
		Ω(k8sClient.Get(ctx, vrepNm, &vrep)).Should(Succeed())

		Ω(vrep.Spec.Source.VerticaDB).Should(Equal(vdb.Name))
		Ω(vrep.Spec.Target.VerticaDB).Should(Equal(vdb.Name))
		Ω(vrep.Spec.Target.SandboxName).Should(Equal(vmeta.GetOnlineUpgradeSandbox(vdb.Annotations)))

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
		Ω(rr.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vmeta.GetOnlineUpgradeReplicator(vdb.Annotations)).Should(Equal(""))
	})

	It("should delete sandbox config map", func() {
		const sbName = "sb1"
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Status = vapi.VerticaDBStatus{
			Sandboxes: []vapi.SandboxStatus{
				{Name: sbName},
			},
		}

		test.CreateConfigMap(ctx, k8sClient, vdb, "", sbName)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sbName)
		rr := &OnlineUpgradeReconciler{
			sandboxName: sbName,
			VDB:         vdb,
			VRec:        vdbRec,
		}
		// requeue because sandbox still exists in the status
		Ω(rr.deleteSandboxConfigMap(ctx)).Should(Equal(ctrl.Result{Requeue: true}))

		nm := names.GenSandboxConfigMapName(rr.VDB, rr.sandboxName)
		cm := &v1.ConfigMap{}
		Expect(k8sClient.Get(ctx, nm, cm)).Should(BeNil())
		rr.VDB.Status.Sandboxes = []vapi.SandboxStatus{}
		Ω(rr.deleteSandboxConfigMap(ctx)).Should(Equal(ctrl.Result{}))
		err := k8sClient.Get(ctx, nm, cm)
		Expect(kerrors.IsNotFound(err)).Should(BeTrue())
	})

	It("should remove the client routing label on replica group A subclusters for the pause", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "sec1", Type: vapi.SecondarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())

		// Before we do the pause, ensure the client routing label is present.
		groupAScNames := rr.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
		Ω(groupAScNames).Should(HaveLen(2))
		pod := v1.Pod{}
		scMap := vdb.GenSubclusterMap()
		for _, scName := range groupAScNames {
			sc, found := scMap[scName]
			Ω(found).Should(BeTrue())
			for i := int32(0); i < sc.Size; i++ {
				nm := names.GenPodName(vdb, sc, i)
				Ω(k8sClient.Get(ctx, nm, &pod)).Should(Succeed())
				Ω(pod.Labels).Should(HaveKeyWithValue(vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal), "podName is %v", nm)
			}
		}

		// Do the pause, which will remove the client routing label
		Ω(rr.pauseConnectionsAtReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))

		// Now check that the routing label was removed for the subclusters in
		// replica group A
		for _, scName := range groupAScNames {
			sc, found := scMap[scName]
			Ω(found).Should(BeTrue())
			for i := int32(0); i < sc.Size; i++ {
				nm := names.GenPodName(vdb, sc, i)
				Ω(k8sClient.Get(ctx, nm, &pod)).Should(Succeed())
				Ω(pod.Labels).ShouldNot(HaveKey(vmeta.ClientRoutingLabel))
			}
		}
	})

	It("should route connections in replica group A to replica group B", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri2", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri3", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "sec1", Type: vapi.SecondarySubcluster, Size: 2},
			{Name: "sec2", Type: vapi.SecondarySubcluster, Size: 2},
			{Name: "sec3", Type: vapi.SecondarySubcluster, Size: 2},
			{Name: "sec4", Type: vapi.SecondarySubcluster, Size: 2},
		}
		vdb.Spec.NMATLSSecret = "tls-abcdef"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateFakeTLSSecret(ctx, vdb, k8sClient, vdb.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, k8sClient, vdb.Spec.NMATLSSecret)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		Ω(rr.assignSubclustersToReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.assignSubclustersToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.runObjReconcilerForMainCluster(ctx)).Should(Equal(ctrl.Result{}))
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		// The above obj reconciler will create a statefulset for each new
		// subcluster. But it doesn't create the pods. That's handled by k8s
		// controller, which doesn't run in this mock environment. So, lets
		// create each of the pods for the new statefulset. No defer is needed
		// for this as we already have a defer to cleanup the pods.
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)

		Ω(rr.sandboxReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Mock completion of sandbox
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		mockCompletionOfSandbox(ctx, vdb)

		Ω(rr.pauseConnectionsAtReplicaGroupA(ctx)).Should(Equal(ctrl.Result{}))
		Ω(rr.redirectConnectionsToReplicaGroupB(ctx)).Should(Equal(ctrl.Result{}))

		// Verify the subclusters and the replica groups
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Spec.Subclusters).Should(HaveLen(14))
		groupAScNames := rr.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupAValue)
		Ω(groupAScNames).Should(HaveLen(7))
		groupBScNames := rr.VDB.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
		Ω(groupBScNames).Should(HaveLen(7))

		// Verify that the client routing label is present only for the
		// subclusters in replica group B.
		scMap := vdb.GenSubclusterMap()
		pod := v1.Pod{}
		for _, scName := range groupAScNames {
			sc, found := scMap[scName]
			Ω(found).Should(BeTrue())
			for i := int32(0); i < sc.Size; i++ {
				nm := names.GenPodName(vdb, sc, i)
				Ω(k8sClient.Get(ctx, nm, &pod)).Should(Succeed(), "podName is %v", nm)
				Ω(pod.Labels).ShouldNot(HaveKey(vmeta.ClientRoutingLabel), "podName is %v", nm)
			}
		}
		for _, scName := range groupBScNames {
			sc, found := scMap[scName]
			Ω(found).Should(BeTrue())
			for i := int32(0); i < sc.Size; i++ {
				nm := names.GenPodName(vdb, sc, i)
				Ω(k8sClient.Get(ctx, nm, &pod)).Should(Succeed(), "podName is %v", nm)
				Ω(pod.Labels).Should(HaveKeyWithValue(vmeta.ClientRoutingLabel, vmeta.ClientRoutingVal), "podName is %v", nm)
			}
		}

		// Verify that the service objects for subclusters in replica group A,
		// route to the pods in replica group B. We build a mapping of each
		// subclusters expected mapping.
		expectedMapping := map[string]string{
			"pri1": "pri1-sb",
			"pri2": "pri2-sb",
			"pri3": "pri3-sb",
			"sec1": "sec1-sb",
			"sec2": "sec2-sb",
			"sec3": "sec3-sb",
			"sec4": "sec4-sb",
		}
		for _, scName := range groupAScNames {
			sc, found := scMap[scName]
			Ω(found).Should(BeTrue())
			expScTarget, found := expectedMapping[sc.Name]
			Ω(found).Should(BeTrue())
			targetSc, found := scMap[expScTarget]
			Ω(found).Should(BeTrue())
			svcNm := names.GenExtSvcName(vdb, sc)
			svc := v1.Service{}
			Ω(k8sClient.Get(ctx, svcNm, &svc)).Should(Succeed(), "svc name is %v", svcNm)
			Ω(svc.Spec.Selector).ShouldNot(HaveKey(vmeta.SubclusterSelectorLabel), "svc name is %v", svcNm)
			Ω(svc.Spec.Selector).Should(HaveKeyWithValue(vmeta.SubclusterSvcNameLabel, targetSc.GetServiceName()), "svc name is %v", svcNm)
		}
	})

	It("should maintain upgrade status messages", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = vapi.OnlineUpgradeVersion
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "sec1", Type: vapi.SecondarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Ω(k8sClient.Update(ctx, vdb)).Should(Succeed())

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		// Drive the upgrade, but we can only get so far.
		Ω(rr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{
			Requeue: false, RequeueAfter: vdb.GetUpgradeRequeueTimeDuration(),
		}))

		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Status.UpgradeStatus).Should(Equal(onlineUpgradeStatusMsgs[createNewSubclustersStatusMsgInx]))

		// Try to update message to earlier one. But status message will stay the same.
		Ω(rr.postStartOnlineUpgradeMsg(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Status.UpgradeStatus).Should(Equal(onlineUpgradeStatusMsgs[createNewSubclustersStatusMsgInx]))

		// Updating to a later message will work though.
		Ω(rr.postSandboxSubclustersMsg(ctx)).Should(Equal(ctrl.Result{}))
		Ω(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		Ω(vdb.Status.UpgradeStatus).Should(Equal(onlineUpgradeStatusMsgs[sandboxSubclustersMsgInx]))
	})

	It("should handle collisions with the sts name", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri2", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.StsNameOverrideAnnotation: fmt.Sprintf("%s-pri2-sb", vdb.Name),
			}},
			{Name: "pri3", Type: vapi.PrimarySubcluster, Size: 2},
			{Name: "pri3-sb-dummy", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.StsNameOverrideAnnotation: fmt.Sprintf("%s-pri3-sb", vdb.Name),
			}},
		}

		rr := createOnlineUpgradeReconciler(ctx, vdb)

		// Test using the sts name with the '-sb' suffix
		stsName, err := rr.genNewSubclusterStsName("pri1-sb", &vdb.Spec.Subclusters[0])
		Ω(err).Should(Succeed())
		Ω(stsName).Should(Equal(fmt.Sprintf("%s-pri1-sb", vdb.Name)))

		// Test oscillating back to the original name
		stsName, err = rr.genNewSubclusterStsName("pri2-sb", &vdb.Spec.Subclusters[1])
		Ω(err).Should(Succeed())
		Ω(stsName).Should(Equal(fmt.Sprintf("%s-pri2", vdb.Name)))

		// Test using a uuid for the sts name
		stsName, err = rr.genNewSubclusterStsName("pri3-sb", &vdb.Spec.Subclusters[2])
		Ω(err).Should(Succeed())
		Ω(stsName).ShouldNot(Equal(fmt.Sprintf("%s-pri3", vdb.Name)))
		Ω(stsName).ShouldNot(Equal(fmt.Sprintf("%s-pri3-sb", vdb.Name)))
		Ω(stsName).Should(HavePrefix(fmt.Sprintf("%s-pri3-sb", vdb.Name)))
	})

	It("should remove subclusters in replica group A in vdb", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupAValue,
			}},
			{Name: "pri2", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupAValue,
			}},
			{Name: "pri1-sb", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupBValue,
			}},
			{Name: "pri2-sb", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupBValue,
			}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		rr := createOnlineUpgradeReconciler(ctx, vdb)

		// subclusters group A should be removed
		Ω(rr.removeReplicaGroupAFromVdb(ctx)).Should(Equal(ctrl.Result{}))

		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		newVdbScNames := []string{}
		for _, sc := range newVdb.Spec.Subclusters {
			newVdbScNames = append(newVdbScNames, sc.Name)
		}
		// only subclusters in group B left
		targetScNames := []string{"pri1-sb", "pri2-sb"}
		Expect(newVdbScNames).Should(ConsistOf(targetScNames))
	})

	It("should rename subclusters in replica group B in vdb", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "pri1-sb", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupBValue,
			}},
			{Name: "pri2-sb", Type: vapi.PrimarySubcluster, Size: 2, Annotations: map[string]string{
				vmeta.ReplicaGroupAnnotation: vmeta.ReplicaGroupBValue,
			}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		rr := createOnlineUpgradeReconciler(ctx, vdb)
		scNameMap := map[string]string{"pri1-sb": "pri1", "pri2-sb": "pri2"}

		// subclusters group A should be removed
		for scInGroupB, scInGroupA := range scNameMap {
			Ω(rr.updateSubclusterNamesInVdb(ctx, scInGroupB, scInGroupA)).Should(BeNil())
		}

		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		newVdbScNames := []string{}
		for _, sc := range newVdb.Spec.Subclusters {
			newVdbScNames = append(newVdbScNames, sc.Name)
		}
		// the subclusters should have the original name in group A
		targetScNames := []string{"pri1", "pri2"}
		Expect(newVdbScNames).Should(ConsistOf(targetScNames))
	})
})

func createOnlineUpgradeReconciler(ctx context.Context, vdb *vapi.VerticaDB) *OnlineUpgradeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := createPodFactsDefault(fpr)
	dispatcher := mockVClusterOpsDispatcher(vdb)

	// Add client-routing labels to all pods that exist
	cr := MakeClientRoutingLabelReconciler(vdbRec, logger, vdb, pfacts, AddNodeApplyMethod, "")
	Ω(cr.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

	actor := MakeOnlineUpgradeReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
	r := actor.(*OnlineUpgradeReconciler)
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

func mockCompletionOfSandbox(ctx context.Context, vdb *vapi.VerticaDB) {
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
}

// mockVClusterOpsDispatcher will create an vcluster-ops dispatcher for test
// purposes.
func mockVClusterOpsDispatcher(vdb *vapi.VerticaDB) vadmin.Dispatcher {
	// We use a function to construct the VClusterProvider. This is called
	// ahead of each API rather than once so that we can setup a custom
	// logger for each API call.
	setupAPIFunc := func(log logr.Logger, apiName string) (vadmin.VClusterProvider, logr.Logger) {
		return &mockvops.MockVClusterOps{}, logr.Logger{}
	}
	evWriter := aterrors.TestEVWriter{}
	return vadmin.MakeVClusterOps(logger, vdb, k8sClient, "pwd", &evWriter, setupAPIFunc)
}
