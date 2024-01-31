/*
 (c) Copyright [2021-2023] Open Text.
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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/atparser"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner/util"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("revivedb_reconcile", func() {
	ctx := context.Background()

	It("should skip reconciler entirely if initPolicy is not revive", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyCreate

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, vdb, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should call revive_db since no db exists", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		sc := &vdb.Spec.Subclusters[0]
		const ScSize = 2
		sc.Size = ScSize
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, ScSize)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		parser := atparser.MakeATParserFromVDB(vdb, logger)
		r.Planr = reviveplanner.MakePlanner(logger, &parser)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		reviveCalls := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "revive_db")
		Expect(len(reviveCalls)).Should(Equal(2)) // 1 for display-only and 1 for the real thing
	})

	It("should include --ignore-cluster-lease in revive_db command", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, vdb, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		vdb.SetIgnoreClusterLease(false)
		opts := r.genReviveOpts(types.NamespacedName{}, []string{"hostA"}, []types.NamespacedName{})
		parms := revivedb.Parms{}
		parms.Make(opts...)
		Expect(parms.IgnoreClusterLease).Should(BeFalse())
		vdb.SetIgnoreClusterLease(true)
		opts = r.genReviveOpts(types.NamespacedName{}, []string{"hostA"}, []types.NamespacedName{})
		parms = revivedb.Parms{}
		parms.Make(opts...)
		Expect(parms.IgnoreClusterLease).Should(BeTrue())
	})

	It("should use reviveOrder to order the host list", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "s0", Size: 3},
			{Name: "s1", Size: 3},
			{Name: "s2", Size: 3},
		}
		vdb.Spec.ReviveOrder = []vapi.SubclusterPodCount{
			{SubclusterIndex: 2, PodCount: 1},
			{SubclusterIndex: 1, PodCount: 2},
			{SubclusterIndex: 0, PodCount: 2},
			{SubclusterIndex: 1, PodCount: 1},
			{SubclusterIndex: 2, PodCount: 2},
			{SubclusterIndex: 0, PodCount: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, vdb, fpr)
		Expect(pfacts.Collect(ctx)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		pods, ok := r.getPodList()
		Expect(ok).Should(BeTrue())
		expectedSubclusterOrder := []string{"s2", "s1", "s1", "s0", "s0", "s1", "s2", "s2", "s0"}
		Expect(len(pods)).Should(Equal(len(expectedSubclusterOrder)))
		for i, expectedSC := range expectedSubclusterOrder {
			Expect(pods[i].subclusterName).Should(Equal(expectedSC), "Subcluster index %d", i)
		}
	})

	It("will generate host list with partial reviveOrder list", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "s0", Size: 3},
			{Name: "s1", Size: 3},
			{Name: "s2", Size: 3},
		}
		vdb.Spec.ReviveOrder = []vapi.SubclusterPodCount{
			{SubclusterIndex: 2, PodCount: 5}, // Will only pick 3 from this subcluster
			{SubclusterIndex: 1, PodCount: 0}, // Will include entire subcluster
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, vdb, fpr)
		Expect(pfacts.Collect(ctx)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		pods, ok := r.getPodList()
		Expect(ok).Should(BeTrue())
		expectedSubclusterOrder := []string{"s2", "s2", "s2", "s1", "s1", "s1", "s0", "s0", "s0"}
		Expect(len(pods)).Should(Equal(len(expectedSubclusterOrder)))
		for i, expectedSC := range expectedSubclusterOrder {
			Expect(pods[i].subclusterName).Should(Equal(expectedSC), "Subcluster index %d", i)
		}
	})

	It("will fail to generate host list if reviveOrder is bad", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, vdb, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)

		vdb.Spec.ReviveOrder = []vapi.SubclusterPodCount{
			{SubclusterIndex: 0, PodCount: 1},
			{SubclusterIndex: 1, PodCount: 1}, // bad as vdb only has a single subcluster
		}
		Expect(pfacts.Collect(ctx)).Should(Succeed())
		_, ok := r.getPodList()
		Expect(ok).Should(BeFalse())
	})

	It("should requeue if there is an incompatible path", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Communal.Path = "/db"
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		atp := atparser.MakeATParserFromVDB(vdb, logger)
		r.Planr = reviveplanner.MakePlanner(logger, &atp)

		// Fake a bad path by changing one in the planr.
		atp.Database.Nodes[0].CatalogPath = "/uncommon-path/vertdb/v_vertdb_node0001_catalog"

		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		reviveCalls := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "revive_db")
		Expect(len(reviveCalls)).Should(Equal(1))
		Expect(reviveCalls[0].Command).Should(ContainElement("--display-only"))
	})

	It("should requeue with correct paths if they differ", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.DBName = "v"
		vdb.Spec.Communal.Path = "/db/dir"
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		atp := atparser.MakeATParserFromVDB(vdb, logger)
		r.Planr = reviveplanner.MakePlanner(logger, &atp)

		// Force a path change in the vdb by changing one in the planr. The
		// planner has the output from revive_db --display-only. That has the
		// correct paths. The planner will update the vdb to match.
		atp.Database.Nodes[0].CatalogPath = "/new-catalog/v/v_v_node0001_catalog"
		atp.Database.Nodes[0].VStorageLocations = []atparser.StorageLocation{
			{Path: "/new-depot/v/v_v_node0001_depot", Usage: util.UsageIsDepot},
			{Path: "/new-data/v/v_v_node0001_data", Usage: util.UsageIsDataTemp},
		}

		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		reviveCalls := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "revive_db")
		Expect(len(reviveCalls)).Should(Equal(1))
		Expect(reviveCalls[0].Command).Should(ContainElement("--display-only"))

		// Fetch the vdb and it should be updated with the new paths
		fetchVdb := vapi.VerticaDB{}
		nm := vdb.ExtractNamespacedName()
		Expect(k8sClient.Get(ctx, nm, &fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Local.DataPath).Should(Equal("/new-data"))
		Expect(fetchVdb.Spec.Local.CatalogPath).Should(Equal("/new-catalog"))
		Expect(fetchVdb.Spec.Local.DepotPath).Should(Equal("/new-depot"))
	})

	It("should delete the sts if pending revision update", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.DBName = "v"
		vdb.Spec.Communal.Path = "/del-pod-chk"
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		sts := &appsv1.StatefulSet{}
		sn := names.GenStsName(vdb, &vdb.Spec.Subclusters[0])
		Expect(k8sClient.Get(ctx, sn, sts)).Should(Succeed())
		// Set two different revisions to force revive to delete the sts
		sts.Status.CurrentRevision = "abcdef1"
		sts.Status.UpdateRevision = "abcdef2"
		Expect(k8sClient.Status().Update(ctx, sts)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)

		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		Expect(k8sClient.Get(ctx, sn, sts)).ShouldNot(Succeed())
	})
})
