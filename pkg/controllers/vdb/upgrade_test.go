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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("upgrade", func() {
	ctx := context.Background()
	const OldImage = "old-image"
	const NewImage = "new-image-1"

	It("should correctly pick the upgrade type", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{
			Name: "sc1", Type: vapi.SecondarySubcluster, Size: 3,
		})
		vdb.Annotations[vmeta.VersionAnnotation] = vapi.ReplicatedUpgradeVersion

		vdb.Spec.UpgradePolicy = vapi.OfflineUpgrade
		Expect(offlineUpgradeAllowed(vdb)).Should(BeTrue())
		Expect(onlineUpgradeAllowed(vdb)).Should(BeFalse())
		Expect(replicatedUpgradeAllowed(vdb)).Should(BeFalse())

		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		Expect(offlineUpgradeAllowed(vdb)).Should(BeFalse())
		Expect(onlineUpgradeAllowed(vdb)).Should(BeTrue())
		Expect(replicatedUpgradeAllowed(vdb)).Should(BeFalse())

		vdb.Spec.UpgradePolicy = vapi.ReplicatedUpgrade
		Expect(offlineUpgradeAllowed(vdb)).Should(BeFalse())
		Expect(onlineUpgradeAllowed(vdb)).Should(BeFalse())
		Expect(replicatedUpgradeAllowed(vdb)).Should(BeTrue())
	})

	It("should not need an upgrade if images match in sts and vdb", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OnlineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(false))
	})

	It("should need an upgrade if images don't match in sts and sandbox", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 2, Type: vapi.PrimarySubcluster},
			{Name: "sc2", Size: 1, Type: vapi.SecondarySubcluster},
		}
		const sbName = "sand"
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName, Image: "new-img", Subclusters: []vapi.SubclusterName{{Name: "sc2"}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{"sc2"}},
		}

		// upgrade not needed on main cluster
		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OnlineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(false))
		// upgrade needed on sandbox
		mgr = MakeUpgradeManager(vdbRec, logger, vdb, vapi.OnlineUpgradeInProgress, sbName,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(true))
	})

	It("should change the image of both primaries and secondaries", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 2, Type: vapi.PrimarySubcluster},
			{Name: "sc2", Size: 3, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(true))
		stsChange, err := mgr.updateImageInStatefulSets(ctx)
		Expect(err).Should(Succeed())
		Expect(stsChange).Should(Equal(2))

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		svrCnt := vk8s.GetServerContainer(sts.Spec.Template.Spec.Containers)
		Expect(svrCnt).ShouldNot(BeNil())
		Expect(svrCnt.Image).Should(Equal(NewImage))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		svrCnt = vk8s.GetServerContainer(sts.Spec.Template.Spec.Containers)
		Expect(svrCnt).ShouldNot(BeNil())
		Expect(svrCnt.Image).Should(Equal(NewImage))
	})

	It("should delete pods of all subclusters", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1, Type: vapi.PrimarySubcluster},
			{Name: "sc2", Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		numPodsDeleted, err := mgr.deletePodsRunningOldImage(ctx, "") // pods from primaries only
		Expect(err).Should(Succeed())
		Expect(numPodsDeleted).Should(Equal(2))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0), pod)).ShouldNot(Succeed())
		Expect(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0), pod)).ShouldNot(Succeed())
	})

	It("should delete pods of specific subcluster", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1, Type: vapi.SecondarySubcluster},
			{Name: "sc2", Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		numPodsDeleted, err := mgr.deletePodsRunningOldImage(ctx, vdb.Spec.Subclusters[1].Name)
		Expect(err).Should(Succeed())
		Expect(numPodsDeleted).Should(Equal(1))

		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0), pod)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0), pod)).ShouldNot(Succeed())

		numPodsDeleted, err = mgr.deletePodsRunningOldImage(ctx, vdb.Spec.Subclusters[0].Name)
		Expect(err).Should(Succeed())
		Expect(numPodsDeleted).Should(Equal(1))

		Expect(k8sClient.Get(ctx, names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0), pod)).ShouldNot(Succeed())
	})

	It("should cleanup upgrade status", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.startUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(mgr.setUpgradeStatus(ctx, "doing the change")).Should(Succeed())

		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.IsStatusConditionTrue(vapi.UpgradeInProgress)).Should(BeTrue())
		Expect(fetchedVdb.IsStatusConditionTrue(vapi.OfflineUpgradeInProgress)).Should(BeTrue())
		Expect(fetchedVdb.Status.UpgradeStatus).ShouldNot(Equal(""))

		Expect(mgr.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.IsStatusConditionFalse(vapi.UpgradeInProgress)).Should(BeTrue())
		Expect(fetchedVdb.IsStatusConditionFalse(vapi.OfflineUpgradeInProgress)).Should(BeTrue())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(""))
	})

	It("should post next status message", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		statusMsgs := []string{"msg1", "msg2", "msg3", "msg4"}

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 1)).Should(Succeed()) // no-op

		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(""))

		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 0)).Should(Succeed())
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(statusMsgs[0]))

		// Skip msg2
		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 2)).Should(Succeed())
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(statusMsgs[2]))

		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 2)).Should(Succeed()) // no change
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(statusMsgs[2]))

		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 3)).Should(Succeed())
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(statusMsgs[3]))

		Expect(mgr.postNextStatusMsg(ctx, statusMsgs, 9)).ShouldNot(Succeed()) // fail - out of bounds
	})

	It("should delete sts if upgrading a monolithic NMA deployment", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = vapi.VcluseropsAsDefaultDeploymentMethodMinVersion
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImage // Change image to force pod deletion
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		sts := &appsv1.StatefulSet{}
		stsnm := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
		Expect(k8sClient.Get(ctx, stsnm, sts)).Should(Succeed())

		pod := &corev1.Pod{}
		podnm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Expect(k8sClient.Get(ctx, podnm, pod)).Should(Succeed())

		// Mock in a status showing the server pod not starting.
		started := false
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: names.ServerContainer,
				Ready:   false,
				Started: &started,
				State: corev1.ContainerState{
					Waiting: &corev1.ContainerStateWaiting{
						Reason:  "CreateContainerError",
						Message: `failed to generate container "abcd" spec: failed to apply OCI options: no command specified'`,
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, pod)).Should(Succeed())

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.changeNMASidecarDeploymentIfNeeded(ctx, sts)).Should(Equal(ctrl.Result{Requeue: true}))

		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Annotations[vmeta.VersionAnnotation]).Should(Equal(vapi.NMAInSideCarDeploymentMinVersion))

		// Verify the sts is deleted
		Expect(k8sClient.Get(ctx, stsnm, sts)).ShouldNot(Succeed())
	})

	It("should clear annotations set for replicated upgrade", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress, vapi.MainCluster,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.startUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		vdb.Spec.Subclusters[0].Annotations = map[string]string{
			vmeta.ReplicaGroupAnnotation:     vmeta.ReplicaGroupAValue,
			vmeta.ParentSubclusterAnnotation: "main",
			vmeta.ChildSubclusterAnnotation:  "child",
		}
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).Should(HaveKey(vmeta.ReplicaGroupAnnotation))
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).Should(HaveKey(vmeta.ParentSubclusterAnnotation))
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).Should(HaveKey(vmeta.ChildSubclusterAnnotation))

		Expect(mgr.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).ShouldNot(HaveKey(vmeta.ReplicaGroupAnnotation))
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).ShouldNot(HaveKey(vmeta.ParentSubclusterAnnotation))
		Expect(fetchedVdb.Spec.Subclusters[0].Annotations).ShouldNot(HaveKey(vmeta.ChildSubclusterAnnotation))
	})
})
