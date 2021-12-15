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
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("onlineimagechange_reconcile", func() {
	ctx := context.Background()
	const OldImage = "old-image"
	const NewImageName = "different-image"

	It("should properly report if primaries don't have matching image in vdb", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		r := createOnlineImageChangeReconciler(vdb)
		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{}))
		Expect(r.allPrimariesHaveNewImage()).Should(BeTrue())
		vdb.Spec.Image = NewImageName
		Expect(r.allPrimariesHaveNewImage()).Should(BeFalse())
	})

	It("should create and delete standby subclusters", func() {
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
		Expect(r.createStandbySts(ctx)).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchVdb))
		defer deletePods(ctx, fetchVdb) // Add to defer again for pods in standbys
		createPods(ctx, fetchVdb, AllPodsRunning)

		fscs := fetchVdb.Spec.Subclusters
		Expect(len(fscs)).Should(Equal(4)) // orig + 1 standbys
		Expect(fscs[3].Name).Should(Equal("sc3-primary-standby"))

		Expect(r.loadSubclusterState(ctx)).Should(Equal(ctrl.Result{})) // Collect state again for new pods/sts

		// Override the pod facts so that newly created pod shows up as not
		// install and db doesn't exist.  This is needed to allow the sts
		// deletion to occur.
		pn := names.GenPodName(fetchVdb, &fscs[3], 0)
		r.PFacts.Detail[pn].isInstalled = tristate.False
		r.PFacts.Detail[pn].dbExists = tristate.False

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &fscs[3]), sts)).Should(Succeed())
		Expect(r.deleteStandbySts(ctx)).Should(Equal(ctrl.Result{}))
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
})

// createOnlineImageChangeReconciler is a helper to run the OnlineImageChangeReconciler.
func createOnlineImageChangeReconciler(vdb *vapi.VerticaDB) *OnlineImageChangeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := MakePodFacts(k8sClient, fpr)
	actor := MakeOnlineImageChangeReconciler(vrec, logger, vdb, fpr, &pfacts)
	return actor.(*OnlineImageChangeReconciler)
}
