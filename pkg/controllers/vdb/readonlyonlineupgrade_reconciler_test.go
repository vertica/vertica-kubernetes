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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("readonlyonlineupgrade_reconcile", func() {
	ctx := context.Background()
	const OldImage = "old-image"
	const NewImageName = "different-image"

	It("should skip transient subcluster setup only when primaries have matching image", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{Name: "transient", Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.skipTransientSetup()).Should(BeTrue())
		r.Vdb.Spec.Image = NewImageName
		Expect(r.skipTransientSetup()).Should(BeFalse())
	})

	It("should create and delete transient subcluster", func() {
		vdb := vapi.MakeVDB()
		scs := []vapi.Subcluster{
			{Name: "sc1-secondary", Type: vapi.SecondarySubcluster, Size: 5},
			{Name: "sc2-secondary", Type: vapi.SecondarySubcluster, Size: 1},
			{Name: "sc3-primary", Type: vapi.PrimarySubcluster, Size: 3},
		}
		vdb.Spec.Subclusters = scs
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{Name: "transient", Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.addTransientToVdb(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.createTransientSts(ctx)).Should(Equal(ctrl.Result{}))

		var nilSc *vapi.Subcluster
		transientSc := vdb.FindTransientSubcluster()
		Expect(transientSc).ShouldNot(Equal(nilSc))

		fetchedSts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, transientSc), fetchedSts)).Should(Succeed())

		Expect(r.removeTransientFromVdb(ctx)).Should(Equal(ctrl.Result{}))
		Expect(vdb.FindTransientSubcluster()).Should(Equal(nilSc))

		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{})) // Collect state again for new pods/sts

		// Override the pod facts so that newly created pod shows up as not
		// install and db doesn't exist.  This is needed to allow the sts
		// deletion to occur.
		pn := names.GenPodName(vdb, transientSc, 0)
		r.PFacts.Detail[pn].SetIsInstalled(false)
		r.PFacts.Detail[pn].SetDBExists(false)

		Expect(r.deleteTransientSts(ctx)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, transientSc), fetchedSts)).ShouldNot(Succeed())
	})

	It("should be able to figure out what the old image was", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		oldImage, ok := r.Manager.fetchOldImage(vapi.MainCluster)
		Expect(ok).Should(BeTrue())
		Expect(oldImage).Should(Equal(OldImage))
	})

	It("should route client traffic to transient subcluster", func() {
		vdb := vapi.MakeVDB()
		const ScName = "sc1"
		const TransientScName = "transient"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, Type: vapi.PrimarySubcluster, Size: 1},
		}
		sc := &vdb.Spec.Subclusters[0]
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{
				Name: TransientScName,
				Size: 1,
				Type: vapi.SecondarySubcluster,
			},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.addTransientToVdb(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.createTransientSts(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.routeClientTraffic(ctx, ScName, true)).Should(Succeed())
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(ContainSubstring(TransientScName))

		// Route back to original subcluster
		Expect(r.routeClientTraffic(ctx, ScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(sc.GetServiceName()))
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(Equal(""))
	})

	It("should not route client traffic to transient subcluster since it doesn't exist", func() {
		vdb := vapi.MakeVDB()
		const ScName = "sc1"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, Type: vapi.PrimarySubcluster, Size: 1},
		}
		sc := &vdb.Spec.Subclusters[0]
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{
				Name: "some-sc-not-to-be-created",
				Size: 1,
				Type: vapi.SecondarySubcluster,
			},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.routeClientTraffic(ctx, ScName, true)).Should(Succeed())
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(ContainSubstring(ScName))
		Expect(svc.Spec.Selector[vmeta.ClientRoutingLabel]).Should(Equal(vmeta.ClientRoutingVal))
	})

	It("should avoid creating transient if the cluster is down", func() {
		vdb := vapi.MakeVDB()
		const ScName = "sc1"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, Type: vapi.PrimarySubcluster, Size: 1},
		}
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{Name: "wont-be-created"},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.skipTransientSetup()).Should(BeTrue())
	})

	It("should route client traffic to existing subcluster", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri"
		const SecScName = "sec"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, Type: vapi.PrimarySubcluster, Size: 1},
			{Name: SecScName, Type: vapi.SecondarySubcluster, Size: 1},
		}
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Names: []string{"dummy-non-existent", SecScName, PriScName},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(PriScName))

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))

		// Route for primary subcluster
		Expect(r.routeClientTraffic(ctx, PriScName, true)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(ContainSubstring(SecScName))
		Expect(r.routeClientTraffic(ctx, PriScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(vdb.Spec.Subclusters[0].GetServiceName()))
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(Equal(""))

		// Route for secondary subcluster
		Expect(r.routeClientTraffic(ctx, SecScName, true)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[1]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSelectorLabel]).Should(ContainSubstring(PriScName))
		Expect(r.routeClientTraffic(ctx, SecScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[1]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[vmeta.SubclusterSvcNameLabel]).Should(Equal(SecScName))
	})

	It("should not match transient subclusters", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri"
		const SecScName = "sec"
		const SubclusterTypeTrue = "true"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, Type: vapi.PrimarySubcluster, Size: 1},
			{Name: SecScName, Type: vapi.SecondarySubcluster, Size: 1},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubcluster)).Should(BeTrue())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubcluster)).Should(BeFalse())

		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubcluster)).Should(BeFalse())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubcluster)).Should(BeTrue())

		sts.Labels[vmeta.SubclusterTypeLabel] = SubclusterTypeTrue // Fake a transient subcluster
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubcluster)).Should(BeFalse())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubcluster)).Should(BeFalse())
	})

	It("should update image in each sts", func() {
		vdb := vapi.MakeVDB()
		const Pri1ScName = "pri1"
		const Pri2ScName = "pri2"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: Pri1ScName, Type: vapi.PrimarySubcluster, Size: 1},
			{Name: Pri2ScName, Type: vapi.PrimarySubcluster, Size: 1},
		}
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Names: []string{Pri2ScName, Pri1ScName},
		}
		vdb.Spec.Image = OldImage
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		vdb.SetIgnoreUpgradePath(true)
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = vapi.ReadOnlyOnlineUpgradeVersion
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		// The reconcile will requeue when it waits for pods to come online that
		// may need a restart.  It would have gotten far enough to update the
		// sts for the primaries.
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(
			ctrl.Result{Requeue: false, RequeueAfter: vdb.GetUpgradeRequeueTimeDuration()}))

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)).Should(Equal(NewImageName))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)).Should(Equal(NewImageName))
	})

	It("should have an upgradeStatus set when it fails part way through", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 1},
		}
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Names: []string{vdb.Spec.Subclusters[0].Name},
		}
		vdb.Spec.Image = OldImage
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = vapi.ReadOnlyOnlineUpgradeVersion
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: false, RequeueAfter: vdb.GetUpgradeRequeueTimeDuration()}))
		Expect(vdb.Status.UpgradeStatus).Should(Equal("Checking if new version is compatible"))
	})

	It("should requeue if there are active connections in the subcluster", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 1},
		}
		sc := &vdb.Spec.Subclusters[0]
		vdb.Spec.Image = OldImage
		vdb.Spec.UpgradePolicy = vapi.ReadOnlyOnlineUpgrade
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		pn := names.GenPodName(vdb, sc, 0)
		Expect(r.PFacts.Collect(ctx, vdb)).Should(Succeed())
		r.PFacts.Detail[pn].SetUpNode(true)
		r.PFacts.Detail[pn].SetReadOnly(false)
		fpr := r.PRunner.(*cmds.FakePodRunner)
		fpr.Results[pn] = []cmds.CmdResult{
			{Stdout: "  5\n"},
		}
		Expect(r.Manager.isSubclusterIdle(ctx, r.PFacts, vdb.Spec.Subclusters[0].Name)).Should(Equal(ctrl.Result{Requeue: true}))
		fpr.Results[pn] = []cmds.CmdResult{
			{Stdout: "  0\n"},
		}
		Expect(r.Manager.isSubclusterIdle(ctx, r.PFacts, vdb.Spec.Subclusters[0].Name)).Should(Equal(ctrl.Result{Requeue: false}))
	})

	It("should requeue after a specified UpgradeRequeueAfter time", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 1},
		}
		vdb.Spec.Image = OldImage
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Names: []string{vdb.Spec.Subclusters[0].Name},
		}
		vdb.Annotations[vmeta.UpgradeRequeueTimeAnnotation] = "100" // Set a non-default UpgradeRequeueTime for the test
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = vapi.ReadOnlyOnlineUpgradeVersion

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: false, RequeueAfter: (time.Second * 100)}))
	})

	It("should return transient in the finder if doing online upgrade", func() {
		vdb := vapi.MakeVDB()
		const ScName = "sc1"
		const TransientScName = "a-transient"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, Type: vapi.PrimarySubcluster, Size: 1},
		}
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{
				Name: TransientScName,
				Size: 1,
				Type: vapi.SecondarySubcluster,
			},
		}
		vdb.Spec.Image = OldImage
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)
		transientSc := vdb.BuildTransientSubcluster("")

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		Expect(r.startUpgrade(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.addTransientToVdb(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.createTransientSts(ctx)).Should(Equal(ctrl.Result{}))

		// Confirm transient exists
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, transientSc), sts)).Should(Succeed())

		scs, err := r.Finder.FindSubclusters(ctx, iter.FindAll|iter.FindSorted, vapi.MainCluster)
		Expect(err).Should(Succeed())
		Expect(len(scs)).Should(Equal(2))
		Expect(scs[0].Name).Should(Equal(TransientScName))
		Expect(scs[1].Name).Should(Equal(ScName))
	})

	It("should route to existing cluster if temporarySubclusterRouting isn't set", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri1"
		const SecScName = "sec2"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, Type: vapi.PrimarySubcluster, Size: 1},
		}

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)
		scMap := vdb.GenSubclusterMap()
		routingSc := r.getSubclusterForTemporaryRouting(ctx, &vdb.Spec.Subclusters[0], scMap)
		Expect(routingSc.Name).Should(Equal(PriScName))

		r.Vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, Type: vapi.PrimarySubcluster, Size: 1},
			{Name: SecScName, Type: vapi.SecondarySubcluster, Size: 1},
		}
		scMap = vdb.GenSubclusterMap()
		routingSc = r.getSubclusterForTemporaryRouting(ctx, &vdb.Spec.Subclusters[0], scMap)
		Expect(routingSc.Name).Should(Equal(SecScName))
		routingSc = r.getSubclusterForTemporaryRouting(ctx, &vdb.Spec.Subclusters[1], scMap)
		Expect(routingSc.Name).Should(Equal(PriScName))
	})

	It("should delete old STS of secondary if online upgrade and changing deployments", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 1},
			{Name: "sc2", Type: vapi.SecondarySubcluster, Size: 1},
		}
		vdb.Spec.Image = OldImage
		vdb.Spec.UpgradePolicy = vapi.OnlineUpgrade
		vdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		vdb.ObjectMeta.Annotations[vmeta.IgnoreUpgradePathAnnotation] = vmeta.IgnoreUpgradePathAnntationTrue

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// Verify we created the sts for the secondary
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())

		// Trigger an upgrade and change the deployment type to vclusterops
		vdb.Spec.Image = NewImageName
		vdb.ObjectMeta.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.ObjectMeta.Annotations[vmeta.VersionAnnotation] = vapi.NMAInSideCarDeploymentMinVersion
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createReadOnlyOnlineUpgradeReconciler(ctx, vdb)

		// Setup the podfacts so that primary is down with new image and
		// secondary is up with old image.
		ppn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		r.PFacts.Detail[ppn].SetAdmintoolsExists(false)
		r.PFacts.Detail[ppn].SetUpNode(false)
		spn := names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
		r.PFacts.Detail[spn].SetAdmintoolsExists(true)
		r.PFacts.Detail[spn].SetUpNode(true)

		// The reconcile will fail because of processing that tries to read the
		// pod, which has been deleted. It depends on the pod being regenerated,
		// which doesn't happen in UT environments.
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// Verify reconciler deleted the old sts
		err := k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)
		Expect(err).ShouldNot(Succeed())
		Expect(errors.IsNotFound(err)).Should(BeTrue())
	})

})

// createReadOnlyOnlineUpgradeReconciler is a helper to run the ReadOnlyOnlineUpgradeReconciler.
func createReadOnlyOnlineUpgradeReconciler(ctx context.Context, vdb *vapi.VerticaDB) *ReadOnlyOnlineUpgradeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
	dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
	actor := MakeReadOnlyOnlineUpgradeReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
	r := actor.(*ReadOnlyOnlineUpgradeReconciler)

	// Ensure one pod is up so that we can do an online upgrade
	Expect(r.PFacts.Collect(ctx, vdb)).Should(Succeed())
	pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
	r.PFacts.Detail[pn].SetUpNode(true)
	r.PFacts.Detail[pn].SetReadOnly(false)

	return r
}
