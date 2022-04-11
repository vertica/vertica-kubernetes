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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/version"
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
		vdb.Spec.TemporarySubclusterRouting.Template = vapi.Subcluster{
			Name:      "t",
			Size:      1,
			IsPrimary: false,
		}

		// No version -- always pick offline
		vdb.Spec.UpgradePolicy = vapi.OfflineUpgrade
		Expect(onlineUpgradeAllowed(vdb)).Should(Equal(false))
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		Expect(onlineUpgradeAllowed(vdb)).Should(Equal(false))
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))

		// auto will pick offline because of no version
		vdb.Spec.UpgradePolicy = vapi.AutoUpgrade
		vdb.Spec.LicenseSecret = "license"
		vdb.Spec.KSafety = vapi.KSafety1
		delete(vdb.ObjectMeta.Annotations, vapi.VersionAnnotation)
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))

		// Older version
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v11.0.0"
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))

		// Correct version
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = version.OnlineUpgradeVersion
		Expect(onlineUpgradeAllowed(vdb)).Should(Equal(true))

		// Missing license
		vdb.Spec.LicenseSecret = ""
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))
		vdb.Spec.LicenseSecret = "license-again"

		// k-safety 0
		vdb.Spec.KSafety = vapi.KSafety0
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))

		// Old version and online requested
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v11.0.2"
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		Expect(offlineUpgradeAllowed(vdb)).Should(Equal(true))
	})

	It("should not need an upgrade if images match in sts and vdb", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OnlineUpgradeInProgress,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(false))
	})

	It("should change the image of both primaries and secondaries", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 2, IsPrimary: true},
			{Name: "sc2", Size: 3, IsPrimary: false},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.IsUpgradeNeeded(ctx)).Should(Equal(true))
		stsChange, res, err := mgr.updateImageInStatefulSets(ctx)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(stsChange).Should(Equal(2))

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImage))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImage))
	})

	It("should delete pods of all subclusters", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1, IsPrimary: true},
			{Name: "sc2", Size: 1, IsPrimary: false},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress,
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
			{Name: "sc1", Size: 1, IsPrimary: false},
			{Name: "sc2", Size: 1, IsPrimary: false},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress,
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

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress,
			func(vdb *vapi.VerticaDB) bool { return true })
		Expect(mgr.startUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(mgr.setUpgradeStatus(ctx, "doing the change")).Should(Succeed())

		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.Conditions[vapi.ImageChangeInProgressIndex].Status).Should(Equal(corev1.ConditionTrue))
		Expect(fetchedVdb.Status.Conditions[vapi.OfflineUpgradeInProgressIndex].Status).Should(Equal(corev1.ConditionTrue))
		Expect(fetchedVdb.Status.UpgradeStatus).ShouldNot(Equal(""))

		Expect(mgr.finishUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.Conditions[vapi.ImageChangeInProgressIndex].Status).Should(Equal(corev1.ConditionFalse))
		Expect(fetchedVdb.Status.Conditions[vapi.OfflineUpgradeInProgressIndex].Status).Should(Equal(corev1.ConditionFalse))
		Expect(fetchedVdb.Status.UpgradeStatus).Should(Equal(""))
	})

	It("should post next status message", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImage // Change image to force pod deletion

		statusMsgs := []string{"msg1", "msg2", "msg3", "msg4"}

		mgr := MakeUpgradeManager(vdbRec, logger, vdb, vapi.OfflineUpgradeInProgress,
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
})
