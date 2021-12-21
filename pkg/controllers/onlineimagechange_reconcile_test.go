/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("onlineimagechange_reconcile", func() {
	ctx := context.Background()
	const OldImage = "old-image"
	const NewImageName = "different-image"

	It("should skip transient subcluster setup only when primaries have matching image", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.skipTransientSetup()).Should(BeTrue())
		vdb.Spec.Image = NewImageName
		Expect(r.skipTransientSetup()).Should(BeFalse())
	})

	It("should create and delete transient subcluster", func() {
		vdb := vapi.MakeVDB()
		scs := []vapi.Subcluster{
			{Name: "sc1-secondary", IsPrimary: false, Size: 5},
			{Name: "sc2-secondary", IsPrimary: false, Size: 1},
			{Name: "sc3-primary", IsPrimary: true, Size: 3},
		}
		vdb.Spec.Subclusters = scs
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		defer deleteSvcs(ctx, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.createTransientSts(ctx)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchVdb))
		defer deletePods(ctx, fetchVdb) // Add to defer again for pods in transient
		createPods(ctx, fetchVdb, AllPodsRunning)

		fscs := fetchVdb.Spec.Subclusters
		Expect(len(fscs)).Should(Equal(4)) // orig + 1 transient
		Expect(fscs[3].Name).Should(Equal(DefaultTransientSubclusterName))

		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{})) // Collect state again for new pods/sts

		// Override the pod facts so that newly created pod shows up as not
		// install and db doesn't exist.  This is needed to allow the sts
		// deletion to occur.
		pn := names.GenPodName(fetchVdb, &fscs[3], 0)
		r.PFacts.Detail[pn].isInstalled = tristate.False
		r.PFacts.Detail[pn].dbExists = tristate.False

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &fscs[3]), sts)).Should(Succeed())
		Expect(r.deleteTransientSts(ctx)).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &fscs[3]), sts)).ShouldNot(Succeed())
	})

	It("should be able to figure out what the old image was", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Image = OldImage
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		oldImage, ok := r.fetchOldImage()
		Expect(ok).Should(BeTrue())
		Expect(oldImage).Should(Equal(OldImage))
	})

	It("should route client traffic to transient subcluster", func() {
		vdb := vapi.MakeVDB()
		const ScName = "sc1"
		const TransientScName = "transient"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: ScName, IsPrimary: true},
		}
		sc := &vdb.Spec.Subclusters[0]
		vdb.Spec.TemporarySubclusterRouting.Template.Name = TransientScName
		vdb.Spec.Image = OldImage
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		createSvcs(ctx, vdb)
		defer deleteSvcs(ctx, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.routeClientTraffic(ctx, ScName, true)).Should(Succeed())
		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[SubclusterNameLabel]).Should(Equal(TransientScName))

		// Route back to original subcluster
		Expect(r.routeClientTraffic(ctx, ScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, sc), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(sc.GetServiceName()))
		Expect(svc.Spec.Selector[SubclusterNameLabel]).Should(Equal(""))
	})

	It("should route client traffic to existing subcluster", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri"
		const SecScName = "sec"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, IsPrimary: true},
			{Name: SecScName, IsPrimary: false},
		}
		vdb.Spec.TemporarySubclusterRouting.Names = []string{"dummy-non-existent", SecScName, PriScName}
		vdb.Spec.Image = OldImage
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		createSvcs(ctx, vdb)
		defer deleteSvcs(ctx, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade

		svc := &corev1.Service{}
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(PriScName))

		r := createOnlineImageChangeReconciler(vdb)

		// Route for primary subcluster
		Expect(r.routeClientTraffic(ctx, PriScName, true)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterTransientLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[SubclusterNameLabel]).Should(Equal(SecScName))
		Expect(r.routeClientTraffic(ctx, PriScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[0]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterTransientLabel]).Should(Equal(""))
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(vdb.Spec.Subclusters[0].GetServiceName()))
		Expect(svc.Spec.Selector[SubclusterNameLabel]).Should(Equal(""))

		// Route for secondary subcluster
		Expect(r.routeClientTraffic(ctx, SecScName, true)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[1]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterNameLabel]).Should(Equal(PriScName))
		Expect(r.routeClientTraffic(ctx, SecScName, false)).Should(Succeed())
		Expect(k8sClient.Get(ctx, names.GenExtSvcName(vdb, &vdb.Spec.Subclusters[1]), svc)).Should(Succeed())
		Expect(svc.Spec.Selector[SubclusterSvcNameLabel]).Should(Equal(SecScName))
	})

	It("should not match transient subclusters", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri"
		const SecScName = "sec"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, IsPrimary: true},
			{Name: SecScName, IsPrimary: false},
		}
		vdb.Spec.Image = OldImage
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)
		vdb.Spec.Image = NewImageName // Trigger an upgrade

		r := createOnlineImageChangeReconciler(vdb)

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubclusterType)).Should(BeTrue())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubclusterType)).Should(BeFalse())

		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubclusterType)).Should(BeFalse())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubclusterType)).Should(BeTrue())

		sts.Labels[SubclusterTypeLabel] = "true" // Fake a transient subcluster
		Expect(r.isMatchingSubclusterType(sts, vapi.PrimarySubclusterType)).Should(BeFalse())
		Expect(r.isMatchingSubclusterType(sts, vapi.SecondarySubclusterType)).Should(BeFalse())
	})

	It("should update image in each sts", func() {
		vdb := vapi.MakeVDB()
		const PriScName = "pri"
		const SecScName = "sec"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: PriScName, IsPrimary: true},
			{Name: SecScName, IsPrimary: false},
		}
		vdb.Spec.TemporarySubclusterRouting.Names = []string{SecScName, PriScName}
		vdb.Spec.Image = OldImage
		vdb.Spec.ImageChangePolicy = vapi.OnlineImageChange
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		vdb.Spec.Image = NewImageName // Trigger an upgrade
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImageName))
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[ServerContainerIndex].Image).Should(Equal(NewImageName))
	})
})

// createOnlineImageChangeReconciler is a helper to run the OnlineImageChangeReconciler.
func createOnlineImageChangeReconciler(vdb *vapi.VerticaDB) *OnlineImageChangeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := MakePodFacts(k8sClient, fpr)
	actor := MakeOnlineImageChangeReconciler(vrec, logger, vdb, fpr, &pfacts)
	return actor.(*OnlineImageChangeReconciler)
}
