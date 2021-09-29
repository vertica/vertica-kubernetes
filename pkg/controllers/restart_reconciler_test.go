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
	"errors"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("restart_reconciler", func() {
	ctx := context.Background()
	const Node1OldIP = "10.10.1.1"
	const Node2OldIP = "10.10.1.2"
	const Node3OldIP = "10.10.1.3"

	It("restartReconciler should not return an error if the sts doesn't exist", func() {
		vdb := vapi.MakeVDB()
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		recon := MakeRestartReconciler(vrec, logger, vdb, fpr, &pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should call restart_node on one pod", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1})

		downPod := &corev1.Pod{}
		downPodNm := names.GenPodName(vdb, sc, 1)
		Expect(k8sClient.Get(ctx, downPodNm, downPod)).Should(Succeed())

		r := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restartCmd := fpr.FindCommands("restart_node")
		Expect(len(restartCmd)).Should(Equal(1))
		Expect(restartCmd[0].Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"restart_node",
			"--new-host-ips="+downPod.Status.PodIP,
		))
	})

	It("should not call restart_node when autoRestartVertica is false", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.AutoRestartVertica = false
		vdb.Spec.Subclusters[0].Size = 2
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		nm := types.NamespacedName{
			Name:      vdb.Name,
			Namespace: vdb.Namespace,
		}

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1})

		r := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		Expect(k8sClient.Get(ctx, nm, vdb)).Should(Succeed())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restartCmd := fpr.FindCommands("restart_node")
		Expect(len(restartCmd)).Should(Equal(0))
		Expect(vdb.Status.Conditions[0].Type).Should(Equal(vapi.AutoRestartVertica))
		Expect(vdb.Status.Conditions[0].Status).Should(Equal(corev1.ConditionFalse))

		// Set back to true to check if  the status is updated accordingly
		vdb.Spec.AutoRestartVertica = true
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, vdb)).Should(Succeed())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Status.Conditions[0].Type).Should(Equal(vapi.AutoRestartVertica))
		Expect(vdb.Status.Conditions[0].Status).Should(Equal(corev1.ConditionTrue))
	})

	It("failure to restart will cause a requeue", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 5
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1, 4})

		// Setup the pod runner to fail the admintools command.
		atPod := names.GenPodName(vdb, sc, 3)
		fpr.Results[atPod] = []cmds.CmdResult{
			{}, // check up node status via -t list_allnodes
			{}, // command that will dump admintools.conf vitals
			{
				Err:    errors.New("all nodes are not down"),
				Stdout: "All nodes in the input are not down, can't restart",
			},
		}

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{
			Requeue:      false,
			RequeueAfter: time.Second * RequeueWaitTimeInSeconds,
		}))
		lastCmd := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCmd.Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"restart_node",
		))
	})

	It("should parse admintools.conf correctly in parseNodesFromAdmintoolsConf", func() {
		ips := parseNodesFromAdmintoolConf(
			"node0001 = 10.244.1.95,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0002 = 10.244.1.96,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0003 = 10.244.1.97,/home/dbadmin/local-data/data/ee65657f-a5f3,/home/dbadmin/local-data/data/ee65657f-a5f3\n" +
				"node0blah = no-ip,/data,/data\n" +
				"node0000 =badly formed\n",
		)
		Expect(ips["node0001"]).Should(Equal("10.244.1.95"))
		Expect(ips["node0002"]).Should(Equal("10.244.1.96"))
		Expect(ips["node0003"]).Should(Equal("10.244.1.97"))
		_, ok := ips["node0004"] // Will not find
		Expect(ok).Should(BeFalse())
		_, ok = ips["node0000"] // Will not find since it was badly formed
		Expect(ok).Should(BeFalse())
	})

	It("should successfully generate a map file from vnodes", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1, 2})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		oldIPs := make(verticaIPLookup)
		oldIPs["node0001"] = Node1OldIP
		oldIPs["node0002"] = Node2OldIP
		oldIPs["node0003"] = Node3OldIP
		mapFileContents, ipChanging, ok := r.genMapFile(oldIPs, pfacts.findReIPPods(false))
		Expect(ok).Should(BeTrue())
		Expect(ipChanging).Should(BeTrue())
		Expect(mapFileContents).Should(ContainElements(
			fmt.Sprintf("%s %s", Node1OldIP, fakeIPForPod(0, 0)),
			fmt.Sprintf("%s %s", Node2OldIP, fakeIPForPod(0, 1)),
			fmt.Sprintf("%s %s", Node3OldIP, fakeIPForPod(0, 2)),
		))
	})

	It("should requeue restart if pods are not running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := &vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		oldIPs := make(verticaIPLookup)
		oldIPs["node0001"] = Node1OldIP
		oldIPs["node0002"] = Node2OldIP
		_, _, ok := r.genMapFile(oldIPs, pfacts.findReIPPods(false))
		Expect(ok).Should(BeFalse())
	})

	It("should only generate a map file for installed pods", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		// Mark one of the pods as uninstalled.  This pod won't be included in the map file
		uninstallPod := names.GenPodName(vdb, sc, 1)
		pfacts.Detail[uninstallPod].isInstalled = tristate.False
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		atPod := names.GenPodName(vdb, sc, 0)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: "node0001 = 10.10.2.1,/d/d\n"},
			},
		}
		oldIPs, err := r.fetchOldIPsFromNode(ctx, atPod)
		Expect(err).Should(Succeed())
		mapFileContents, ipChanging, ok := r.genMapFile(oldIPs, pfacts.findReIPPods(false))
		Expect(ok).Should(BeTrue())
		Expect(ipChanging).Should(BeTrue())
		Expect(len(mapFileContents)).Should(Equal(1))
		Expect(mapFileContents).Should(ContainElement(
			"10.10.2.1 " + fakeIPForPod(0, 0),
		))
	})

	It("should successfully generate a map file from compat21 nodes", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		atPod := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1, 2})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: "node0001 = 10.10.2.1,/d/d\nnode0002 = 10.10.2.2,/d,/d\nnode0003 = 10.10.2.3,/d,/d\n"},
			},
		}
		oldIPs, err := r.fetchOldIPsFromNode(ctx, atPod)
		Expect(err).Should(Succeed())
		mapFileContents, ipChanging, ok := r.genMapFile(oldIPs, pfacts.findReIPPods(false))
		Expect(ok).Should(BeTrue())
		Expect(ipChanging).Should(BeTrue())
		Expect(mapFileContents).Should(ContainElements(
			"10.10.2.1 "+fakeIPForPod(0, 0),
			"10.10.2.2 "+fakeIPForPod(0, 1),
			"10.10.2.3 "+fakeIPForPod(0, 2),
		))
	})

	It("should not detect that map file has no IPs that are changing", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		atPod := names.GenPodName(vdb, sc, 0)
		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: fmt.Sprintf("node0001 = %s,/d/d\nnode0002 = %s,/d,/d\n", fakeIPForPod(0, 0), fakeIPForPod(0, 1))},
			},
		}
		oldIPs, err := r.fetchOldIPsFromNode(ctx, atPod)
		Expect(err).Should(Succeed())
		_, ipChanging, ok := r.genMapFile(oldIPs, pfacts.findReIPPods(false))
		Expect(ok).Should(BeTrue())
		Expect(ipChanging).Should(BeFalse())
	})

	It("should upload a map file, call re_ip then start_db", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		vdb.Spec.DBName = "vertdb"
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		// Setup the pod runner to grep out admintools.conf
		atPod := names.GenPodName(vdb, sc, 3)
		fpr.Results[atPod] = []cmds.CmdResult{
			{
				Stdout: "node0001 = 4.4.4.4,/d,/d\nnode0002 = 5.5.5.5,/f,/f\n",
			},
		}

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// Check the command history.
		upload := fpr.FindCommands("4.4.4.4", fakeIPForPod(0, 0)) // Verify we upload the map file
		Expect(len(upload)).Should(Equal(1))
		reip := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "re_ip")
		Expect(len(reip)).Should(Equal(1))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "start_db")
		Expect(len(restart)).Should(Equal(1))
	})

	It("should parse the list_allnodes output", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.DBName = "d"

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := MakePodFacts(k8sClient, fpr)
		act := MakeRestartReconciler(vrec, logger, vdb, fpr, &pfacts)
		r := act.(*RestartReconciler)
		stateMap := r.parseClusterNodeStatus(
			" Node          | Host       | State | Version                 | DB \n" +
				"---------------+------------+-------+-------------------------+----\n" +
				" v_d_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db \n" +
				" v_d_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | db \n" +
				"\n",
		)
		n1, ok := stateMap["v_d_node0001"]
		Expect(ok).Should(BeTrue())
		Expect(n1).Should(Equal("UP"))
		n2, ok := stateMap["v_d_node0002"]
		Expect(ok).Should(BeTrue())
		Expect(n2).Should(Equal("DOWN"))
	})

	It("should avoid start_db since cluster state still says a host is up", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		vdb.Spec.DBName = "db"
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		atPod := names.GenPodName(vdb, sc, 3)
		fpr.Results[atPod] = []cmds.CmdResult{
			{}, // re-ip command
			{Stdout: " Node          | Host       | State | Version                 | DB \n" +
				"---------------+------------+-------+-------------------------+----\n" +
				" v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | d \n" +
				" v_db_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | d \n" +
				"\n",
			},
		}

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		listCmd := fpr.FindCommands("list_allnodes")
		Expect(len(listCmd)).Should(Equal(1))
	})

	It("should avoid restart_node since cluster state still says the host is up", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		vdb.Spec.DBName = "b"
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		sc := &vdb.Spec.Subclusters[0]
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const DownPodIndex = 0
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{DownPodIndex})
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		atPod := names.GenPodName(vdb, sc, 3)
		fpr.Results[atPod] = []cmds.CmdResult{
			{Stdout: " Node          | Host       | State | Version                 | DB \n" +
				"---------------+------------+-------+-------------------------+----\n" +
				" v_b_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | b \n" +
				" v_b_node0002 | 10.244.1.7 | UP    | vertica-11.0.0.20210309 | b \n" +
				"\n",
			},
		}

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		lastCmd := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCmd.Command).Should(ContainElement("list_allnodes"))
	})

	It("should call start_db with --ignore-cluster-lease and --timeout options", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.IgnoreClusterLease = true
		vdb.Spec.RestartTimeout = 500
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1})
		atPod := names.GenPodName(vdb, sc, 0)

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.restartCluster(ctx)).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "start_db")
		Expect(len(restart)).Should(Equal(1))
		Expect(restart[0].Command).Should(ContainElements("--ignore-cluster-lease"))
		Expect(restart[0].Command).Should(ContainElements("--timeout=500"))
	})

	It("should call restart_node with --timeout option", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.RestartTimeout = 800
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0})

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		Expect(r.reconcileNodes(ctx)).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(1))
		Expect(restart[0].Command).Should(ContainElements("--timeout=800"))
	})

	It("should call re_ip for pods that haven't installed the db", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		const ScSize = 2
		sc.Size = ScSize
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		atPod := types.NamespacedName{}
		for i := 0; i < ScSize; i++ {
			nm := names.GenPodName(vdb, sc, int32(i))
			if pfacts.Detail[nm].dbExists.IsTrue() {
				atPod = nm
				break
			}
		}
		fpr.Results[atPod] = []cmds.CmdResult{
			{
				Stdout: "node0001 = 4.4.4.4,/d,/d\nnode0002 = 5.5.5.5,/f,/f\n",
			},
		}

		act := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		r := act.(*RestartReconciler)
		r.ATPod = atPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		reip := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "re_ip")
		Expect(len(reip)).Should(Equal(1))
	})

	It("should requeue if one pod is not running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		sc := &vdb.Spec.Subclusters[0]
		const ScSize = 2
		sc.Size = ScSize
		createVdb(ctx, vdb)
		defer deleteVdb(ctx, vdb)
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)

		// Pod -0 is running and pod -1 is not running.
		setPodStatusHelper(ctx, 1, names.GenPodName(vdb, sc, 0), 0, 0, AllPodsRunning, false)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const DownPodIndex = 1
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{DownPodIndex})

		r := MakeRestartReconciler(vrec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})
})
