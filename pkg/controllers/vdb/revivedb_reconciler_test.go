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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("revivedb_reconcile", func() {
	ctx := context.Background()

	It("should skip reconciler entirely if initPolicy is not revive", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyCreate

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		r := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should skip calling revive_db if db exists", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, InstallCount: sc.Size, AddedToDBCount: sc.Size},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		r := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		reviveCalls := fpr.FindCommands("/opt/vertica/bin/admintools", "revive_db")
		Expect(len(reviveCalls)).Should(Equal(0))
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
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		r.Planr = reviveplanner.MakeATPlannerFromVDB(vdb, logger)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		reviveCalls := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "revive_db")
		Expect(len(reviveCalls)).Should(Equal(2)) // 1 for display-only and 1 for the real thing
	})

	It("should generate a requeue error for various known s3 errors", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)

		errStrings := []string{
			"Error: The database vertdb cannot continue because the communal storage location\n\ts3://nimbusdb/db\n" +
				"might still be in use.\n\nthe cluster lease will expire:\n\t2021-05-13 14:35:00.280925",
			"Could not copy file [s3://nimbusdb/db/empty/metadata/newdb/cluster_config.json] to [/tmp/desc.json]: " +
				"No such file or directory [s3://nimbusdb/db/empty/metadata/newdb/cluster_config.json]",
			"Could not copy file [gs://vertica-fleeting/mspilchen/revivedb-failures/metadata/vertdb/cluster_conf] to [/tmp/desc.json]: " +
				"File not found",
			"Could not copy file [webhdfs://vertdb/cluster_config.json] to [/tmp/desc.json]: Seen WebHDFS exception: " +
				"\nURL: [http://vertdb/cluster_config.json]\nHTTP response code: 404\nException type: FileNotFoundException",
			"Could not copy file [azb://cluster_config.json] to [/tmp/desc.json]: : The specified blob does not exist",
			"\n10.244.1.34 Permission Denied \n\n",
			"Database could not be revived.\nError: Node count mismatch",
			"Error: Primary node count mismatch:",
			"Could not copy file [s3://nimbusdb/db/spilly/metadata/vertdb/cluster_config.json] to [/tmp/desc.json]: Unable to connect to endpoint\n",
			"[/tmp/desc.json]: The specified bucket does not exist\nExit",
		}

		for i := range errStrings {
			fpr.Results = cmds.CmdResults{
				atPod: []cmds.CmdResult{
					{
						Stdout: errStrings[i],
						Err:    errors.New("at command failed"),
					},
				},
			}
			Expect(r.execCmd(ctx, atPod, []string{"revive_db"})).Should(Equal(ctrl.Result{Requeue: true}), "Failing with '%s'", errStrings[i])
		}

		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{
					Stdout: "*** Unknown error",
					Err:    errors.New("at command failed"),
				},
			},
		}
		res, err := r.execCmd(ctx, atPod, []string{"create_db"})
		Expect(err).ShouldNot(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
	})

	It("should include --ignore-cluster-lease in revive_db command", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		vdb.Spec.IgnoreClusterLease = false
		opts := r.genReviveOpts(types.NamespacedName{}, []string{"hostA"})
		parms := revivedb.Parms{}
		parms.Make(opts...)
		Expect(parms.IgnoreClusterLease).Should(BeFalse())
		vdb.Spec.IgnoreClusterLease = true
		opts = r.genReviveOpts(types.NamespacedName{}, []string{"hostA"})
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
		pfacts := MakePodFacts(vdbRec, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
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
		pfacts := MakePodFacts(vdbRec, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
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
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)

		vdb.Spec.ReviveOrder = []vapi.SubclusterPodCount{
			{SubclusterIndex: 0, PodCount: 1},
			{SubclusterIndex: 1, PodCount: 1}, // bad as vdb only has a single subcluster
		}
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
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
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		r.Planr = reviveplanner.MakeATPlannerFromVDB(vdb, logger)

		// Fake a bad path by changing one in the planr.
		atp := r.Planr.(*reviveplanner.ATPlanner)
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
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)
		r := act.(*ReviveDBReconciler)
		r.Planr = reviveplanner.MakeATPlannerFromVDB(vdb, logger)

		// Force a path change in the vdb by changing one in the planr. The
		// planner has the output from revive_db --display-only. That has the
		// correct paths. The planner will update the vdb to match.
		atp := r.Planr.(*reviveplanner.ATPlanner)
		atp.Database.Nodes[0].CatalogPath = "/new-catalog/v/v_v_node0001_catalog"
		atp.Database.Nodes[0].VStorageLocations = []reviveplanner.StorageLocation{
			{Path: "/new-depot/v/v_v_node0001_depot", Usage: reviveplanner.UsageIsDepot},
			{Path: "/new-data/v/v_v_node0001_data", Usage: reviveplanner.UsageIsDataTemp},
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

	It("should delete the pod if pending revision update", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.DBName = "v"
		vdb.Spec.Communal.Path = "/del-pod-chk"
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pn].stsRevisionPending = true
		act := MakeReviveDBReconciler(vdbRec, logger, vdb, fpr, pfacts, dispatcher)

		pod := corev1.Pod{}
		Expect(k8sClient.Get(ctx, pn, &pod)).Should(Succeed())
		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		Expect(k8sClient.Get(ctx, pn, &pod)).ShouldNot(Succeed())
	})
})
