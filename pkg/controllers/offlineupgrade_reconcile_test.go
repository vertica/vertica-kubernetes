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

package controllers

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("offlineupgrade_reconcile", func() {
	ctx := context.Background()

	It("should change image if image don't match between sts and vdb", func() {
		vdb := vapi.MakeVDB()
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		const NewImage = "vertica-k8s:newimage"

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image).ShouldNot(Equal(NewImage))

		updateVdbToCauseUpgrade(ctx, vdb, NewImage)

		r, _, _ := createOfflineUpgradeReconciler(vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(sts.Spec.Template.Spec.Containers[names.ServerContainerIndex].Image).Should(Equal(NewImage))
	})

	It("should stop cluster during an upgrade", func() {
		vdb := vapi.MakeVDB()
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		updateVdbToCauseUpgrade(ctx, vdb, "container1:newimage")

		r, fpr, _ := createOfflineUpgradeReconciler(vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		h := fpr.FindCommands("admintools -t stop_db")
		Expect(len(h)).Should(Equal(1))
	})

	It("should requeue upgrade if pods aren't running", func() {
		vdb := vapi.MakeVDB()
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		updateVdbToCauseUpgrade(ctx, vdb, "container2:newimage")

		r, _, _ := createOfflineUpgradeReconciler(vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		// Delete the sts in preparation of recrating everything with the new
		// image.  Pods will come up not running to force a requeue by the
		// restart reconciler.
		deletePods(ctx, vdb)
		createPods(ctx, vdb, AllPodsNotRunning)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should delete pods during an upgrade", func() {
		vdb := vapi.MakeVDB()
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)

		updateVdbToCauseUpgrade(ctx, vdb, "container2:newimage")

		r, _, _ := createOfflineUpgradeReconciler(vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		finder := MakeSubclusterFinder(k8sClient, vdb)
		pods, err := finder.FindPods(ctx, FindExisting)
		Expect(err).Should(Succeed())
		Expect(len(pods.Items)).Should(Equal(0))
	})

	It("should avoid stop_db if vertica isn't running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		updateVdbToCauseUpgrade(ctx, vdb, "container2:newimage")
		r, fpr, pfacts := createOfflineUpgradeReconciler(vdb)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfacts.Detail[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)].upNode = false
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		h := fpr.FindCommands("admintools -t stop_db")
		Expect(len(h)).Should(Equal(0))
	})

	It("should set continuingUpgrade if calling reconciler again after failure", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 1
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		updateVdbToCauseUpgrade(ctx, vdb, "container3:newimage")
		r, fpr, pfacts := createOfflineUpgradeReconciler(vdb)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())

		// Fail stop_db so that the reconciler fails
		pn := names.GenPodName(vdb, sc, 0)
		fpr.Results[pn] = append(fpr.Results[pn], cmds.CmdResult{Err: fmt.Errorf("stop_db fails")})

		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(r.Manager.ContinuingUpgrade).Should(Equal(false))

		// Read the latest vdb to get status conditions, etc.
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), vdb)).Should(Succeed())

		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		Expect(r.Manager.ContinuingUpgrade).Should(Equal(true))
	})
})

// updateVdbToCauseUpgrade is a helper to force the upgrade reconciler to do work
func updateVdbToCauseUpgrade(ctx context.Context, vdb *vapi.VerticaDB, newImage string) {
	ExpectWithOffset(1, k8sClient.Get(ctx, vapi.MakeVDBName(), vdb)).Should(Succeed())
	vdb.Spec.Image = newImage
	ExpectWithOffset(1, k8sClient.Update(ctx, vdb)).Should(Succeed())
}

// createOfflineUpgradeReconciler is a helper to run the OfflineUpgradeReconciler.
func createOfflineUpgradeReconciler(vdb *vapi.VerticaDB) (*OfflineUpgradeReconciler, *cmds.FakePodRunner, *PodFacts) {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := MakePodFacts(k8sClient, fpr)
	actor := MakeOfflineUpgradeReconciler(vrec, logger, vdb, fpr, &pfacts)
	return actor.(*OfflineUpgradeReconciler), fpr, &pfacts
}
