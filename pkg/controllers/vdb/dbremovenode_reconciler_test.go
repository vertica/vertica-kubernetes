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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("dbremovenode_reconcile", func() {
	ctx := context.Background()

	It("dbremovenode should not return an error if the sts doesn't exist", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		recon := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should call db_remove_node on one pod", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		uninstallPod := builder.BuildPod(vdb, sc, 1)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		actor := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		recon := actor.(*DBRemoveNodeReconciler)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Histories = make([]cmds.CmdHistory, 0) // reset the calls so the first one is admintools
		_, err := recon.removeNodesInSubcluster(ctx, sc, 1, 1)
		Expect(err).Should(Succeed())
		Expect(fpr.Histories[0].Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"db_remove_node",
			"--hosts",
			fmt.Sprintf("%s.%s.%s", uninstallPod.Spec.Hostname, uninstallPod.Spec.Subdomain, uninstallPod.Namespace),
		))
	})

	It("should call db_remove_node on multiple pods", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)

		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, Detail: []vapi.VerticaDBPodStatus{
				{AddedToDB: true},
				{AddedToDB: true},
				{AddedToDB: true},
			}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		// Resize the subcluster to remove two nodes
		nm := vdb.ExtractNamespacedName()
		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, nm, fetchedVdb)).Should(Succeed())
		fetchedVdb.Spec.Subclusters[0].Size = 1
		Expect(k8sClient.Update(ctx, fetchedVdb)).Should(Succeed())

		uninstallPods := []types.NamespacedName{
			names.GenPodName(fetchedVdb, sc, 1), names.GenPodName(fetchedVdb, sc, 2),
		}

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, fetchedVdb)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, fetchedVdb, fpr, TestPassword)
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, fetchedVdb, fpr, pfacts, dispatcher)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"db_remove_node",
			"--hosts",
			fmt.Sprintf("%s,%s", pfacts.Detail[uninstallPods[0]].GetDNSName(), pfacts.Detail[uninstallPods[1]].GetDNSName()),
		))

		Expect(k8sClient.Get(ctx, nm, fetchedVdb)).Should(Succeed())
		Expect(len(fetchedVdb.Status.Subclusters[0].Detail)).Should(Equal(int(sc.Size)))
		Expect(fetchedVdb.Status.Subclusters[0].Detail[0].AddedToDB).Should(BeTrue())
		Expect(fetchedVdb.Status.Subclusters[0].Detail[1].AddedToDB).Should(BeFalse())
		Expect(fetchedVdb.Status.Subclusters[0].Detail[2].AddedToDB).Should(BeFalse())
	})

	It("should skip remove node and requeue because pod we need to remove node from isn't running", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, AddedToDBCount: sc.Size, Detail: []vapi.VerticaDBPodStatus{{Installed: true}, {Installed: true}}},
		}
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)
		sc.Size-- // mimic a pending db_remove_node

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should skip remove node if pod never had the db added", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)
		sc.Size = 2 // Set to 2 to mimic a pending uninstall of the last pod

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		removePod := names.GenPodName(vdb, sc, 2)
		pfacts.Detail[removePod].SetDBExists(false)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).ShouldNot(ContainElements("/opt/vertica/bin/admintools", "db_remove_node"))
	})
})
