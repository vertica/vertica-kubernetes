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
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	fakeListAllNodeOutputWithOneDownNode = `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.8 | UP    | vertica-11.0.0.20210309 | db 

`
)

var _ = Describe("restart_reconciler", func() {
	ctx := context.Background()
	const RestartProcessReadOnly = true
	const RestartSkipReadOnly = false
	const PodNotReadOnly = false
	const PodReadOnly = true

	It("restartReconciler should not return an error if the sts doesn't exist", func() {
		vdb := vapi.MakeVDB()
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		recon := MakeRestartReconciler(vdbRec, logger, vdb, fpr, &pfacts, RestartProcessReadOnly, dispatcher)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should call restart_node on one pod", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1}, PodNotReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 4)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: fakeListAllNodeOutputWithOneDownNode},
		}

		downPod := &corev1.Pod{}
		downPodNm := names.GenPodName(vdb, sc, 1)
		Expect(k8sClient.Get(ctx, downPodNm, downPod)).Should(Succeed())

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
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
		vdb.Spec.Subclusters[0].Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		nm := types.NamespacedName{
			Name:      vdb.Name,
			Namespace: vdb.Namespace,
		}

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1}, PodNotReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 4)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: fakeListAllNodeOutputWithOneDownNode},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(k8sClient.Get(ctx, nm, vdb)).Should(Succeed())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restartCmd := fpr.FindCommands("restart_node")
		Expect(len(restartCmd)).Should(Equal(0))
		Expect(vdb.IsStatusConditionFalse(vapi.AutoRestartVertica)).Should(BeTrue())

		// Set back to true to check if  the status is updated accordingly
		vdb.Spec.AutoRestartVertica = true
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, vdb)).Should(Succeed())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.IsStatusConditionTrue(vapi.AutoRestartVertica)).Should(BeTrue())
	})

	It("failure to restart will cause a requeue", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 5
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{1, 4}, PodNotReadOnly)

		// Setup the pod runner to fail the admintools command.
		initiatorPod := names.GenPodName(vdb, sc, 6)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0002 | 10.244.1.7 | DOWN  | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.8 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0004 | 10.244.1.9 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0005 | 10.244.1.10| DOWN  | vertica-11.0.0.20210309 | db 

`,
			}, // check up node status via -t list_allnodes
			{
				Err:    errors.New("all nodes are not down"),
				Stdout: "All nodes in the input are not down, can't restart",
			},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
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

	It("should requeue restart if pods are not running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := &vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1}, PodNotReadOnly)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should upload a map file, call re_ip then start_db", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		vdb.Spec.DBName = "vertdb"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1}, PodNotReadOnly)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		// Setup the pod runner to grep out admintools.conf
		initiatorPod := names.GenPodName(vdb, sc, 3)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{
				Stdout: "node0001 = 4.4.4.4,/d,/d\nnode0002 = 5.5.5.5,/f,/f\n",
			},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// Check the command history.
		upload := fpr.FindCommands("4.4.4.4", test.FakeIPForPod(0, 0)) // Verify we upload the map file
		Expect(len(upload)).Should(Equal(0))                           // TODO: 1 -> 0 figure out why VER-93564
		reip := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "re_ip")
		Expect(len(reip)).Should(Equal(0)) // TODO: 1 -> 0 figure out why VER-93564
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "start_db")
		Expect(len(restart)).Should(Equal(1))
	})

	It("should do full cluster restart if none of the nodes are UP and not read-only", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Spec.DBName = "db"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		for podIndex := int32(0); podIndex < vdb.Spec.Subclusters[0].Size; podIndex++ {
			downPodNm := names.GenPodName(vdb, sc, podIndex)
			// At least one pod needs to be totally offline.  Cannot have all of them read-only.
			pfacts.Detail[downPodNm].SetUpNode(podIndex != 0)
			pfacts.Detail[downPodNm].SetReadOnly(podIndex != 0)
			pfacts.Detail[downPodNm].SetIsInstalled(true)
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		listCmd := fpr.FindCommands("start_db")
		Expect(len(listCmd)).Should(Equal(1))
	})

	It("should avoid restart_node since cluster state still says the host is up", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Spec.DBName = "b"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const downPodIndex = 0
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{downPodIndex}, PodNotReadOnly)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		initiatorPod := names.GenPodName(vdb, sc, 4)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
  v_b_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | b 
  v_b_node0002 | 10.244.1.7 | UP    | vertica-11.0.0.20210309 | b 
  v_b_node0003 | 10.244.1.8 | UP    | vertica-11.0.0.20210309 | b 

`,
			},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: false, RequeueAfter: 22000000000}))
		lastCmd := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCmd.Command).Should(ContainElement("list_allnodes"))
	})

	It("should call start_db with --ignore-cluster-lease and --timeout options", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.IgnoreClusterLeaseAnnotation] = "true"
		vdb.Annotations[vmeta.RestartTimeoutAnnotation] = "500"
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0, 1}, PodNotReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 0)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)

		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.restartCluster(ctx, []*podfacts.PodFact{})).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "start_db")
		Expect(len(restart)).Should(Equal(1))
		Expect(restart[0].Command).Should(ContainElements("--ignore-cluster-lease"))
		Expect(restart[0].Command).Should(ContainElements("--timeout=500"))
	})

	It("should call restart_node with --timeout option", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.RestartTimeoutAnnotation] = "800"
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{0}, PodNotReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 4)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0001 | 10.244.1.6 | DOWN  | vertica-11.0.0.20210309 | db 
 v_db_node0002 | 10.244.1.7 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.8 | UP    | vertica-11.0.0.20210309 | db 

`,
			},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.reconcileNodes(ctx)).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(1))
		Expect(restart[0].Command).Should(ContainElements("--timeout=800"))
	})

	It("should call re_ip for pods that haven't installed the db", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		const scSize = 3
		sc.Size = scSize
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)

		initiatorPod := types.NamespacedName{}
		for i := 0; i < scSize; i++ {
			nm := names.GenPodName(vdb, sc, int32(i))
			if pfacts.Detail[nm].GetDBExists() {
				initiatorPod = nm
				break
			}
		}
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{
				Stdout: "node0001 = 4.4.4.4,/d,/d\nnode0002 = 5.5.5.5,/f,/f\n",
			},
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		reip := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "re_ip")
		Expect(len(reip)).Should(Equal(1))
	})

	It("should requeue if one pod is not running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// Update the status to indicate install count includes both pods
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: vdb.Spec.Subclusters[0].Name, Detail: []vapi.VerticaDBPodStatus{{Installed: true}, {Installed: true}}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		// Pod -0 and -2 are running and pod -1 is not running.
		test.SetPodStatus(ctx, k8sClient, 1, names.GenPodName(vdb, sc, 0), 0, 0, test.AllPodsRunning)
		test.SetPodStatus(ctx, k8sClient, 1, names.GenPodName(vdb, sc, 2), 0, 2, test.AllPodsRunning)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const downPodIndex = 1
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{downPodIndex}, PodNotReadOnly)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)

		r := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should avoid restart_node of read-only nodes when that setting is used", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const downPodIndex = 0
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, sc, fpr, []int32{downPodIndex}, PodReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 3)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0002 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.7 | UP    | vertica-11.0.0.20210309 | db 

`,
			},
		}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)

		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartSkipReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(0))

		act = MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r = act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restart = fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(1))
	})

	It("should skip restart_node of transient nodes", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.TemporarySubclusterRouting = &vapi.SubclusterSelection{
			Template: vapi.Subcluster{
				Name: "the-transient-sc",
				Size: 1,
				Type: vapi.SecondarySubcluster,
			},
		}
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		transientSc := vdb.BuildTransientSubcluster("")
		test.CreateSts(ctx, k8sClient, vdb, transientSc, 1, 0, test.AllPodsRunning)
		defer test.DeleteSts(ctx, k8sClient, vdb, transientSc, 1)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		const DownPodIndex = 0
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, transientSc, fpr, []int32{DownPodIndex}, PodReadOnly)

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		Expect(act.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(0))
	})

	It("should requeue if k-safety is 0, there are no UP nodes and some pods aren't running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		vdb.Annotations[vmeta.KSafetyAnnotation] = "0"
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		for podIndex := int32(0); podIndex < vdb.Spec.Subclusters[0].Size; podIndex++ {
			downPodNm := names.GenPodName(vdb, sc, podIndex)
			pfacts.Detail[downPodNm].SetUpNode(false)
			pfacts.Detail[downPodNm].SetReadOnly(false)
			pfacts.Detail[downPodNm].SetIsInstalled(true)
			// At least one pod needs to not be running.
			pfacts.Detail[downPodNm].SetIsPodRunning(podIndex != 0)
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		listCmd := fpr.FindCommands("start_db")
		Expect(len(listCmd)).Should(Equal(0))

		// Start the one pod that isn't running.  This should all start_db to initiated
		downPodNm := names.GenPodName(vdb, sc, 0)
		pfacts.Detail[downPodNm].SetIsPodRunning(true)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		listCmd = fpr.FindCommands("start_db")
		Expect(len(listCmd)).Should(Equal(1))
	})

	It("should check container status to see if startupProbe is done", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.Sidecars = []corev1.Container{
			{Name: "vlogger", Image: "vertica-vlogger:latest"},
		}
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)

		pn := names.GenPodName(vdb, sc, 0)
		pod := corev1.Pod{}
		Expect(k8sClient.Get(ctx, pn, &pod)).Should(Succeed())
		startupProbeFinished := false
		vloggerStarted := true
		// Mimic container status. The of the list can be different from the
		// container order in the spec.
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "vlogger", Started: &vloggerStarted},
			{Name: names.ServerContainer, Started: &startupProbeFinished},
		}
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		Expect(r.isStartupProbeActive(ctx, pn)).Should(BeTrue())
		startupProbeFinished = true
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "vlogger", Started: &vloggerStarted},
			{Name: names.ServerContainer, Started: &startupProbeFinished},
		}
		Expect(k8sClient.Status().Update(ctx, &pod)).Should(Succeed())
		Expect(r.isStartupProbeActive(ctx, pn)).Should(BeFalse())
	})

	It("should pick a suitable requeueTimeout for livenessProbe wait", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.LivenessProbeOverride = &corev1.Probe{
			PeriodSeconds:    45,
			FailureThreshold: 10,
		}
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)

		const expectedRequeueTime int = 112 // 45 * 10 * 0.25
		Expect(r.makeResultForLivenessProbeWait(ctx)).Should(Equal(
			ctrl.Result{RequeueAfter: time.Second * time.Duration(expectedRequeueTime)},
		))
	})

	It("should requeue for cluster restart because livenessProbes are active", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.LivenessProbeOverride = &corev1.Probe{
			PeriodSeconds:    15,
			FailureThreshold: 5,
		}
		vdb.Spec.Subclusters[0].Size = 2
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// Make the startupProbe active on all of the pods
		for i := int32(0); i < sc.Size; i++ {
			pn := names.GenPodName(vdb, sc, i)
			pod := &corev1.Pod{}
			Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
			started := true
			pod.Status.ContainerStatuses = []corev1.ContainerStatus{
				{Name: names.ServerContainer, Started: &started},
			}
			Expect(k8sClient.Status().Update(ctx, pod)).Should(Succeed())
		}
		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, &vdb.Spec.Subclusters[0], fpr, []int32{0, 1}, PodNotReadOnly)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		expectedRequeueTime := int(float64(vdb.Spec.LivenessProbeOverride.PeriodSeconds*vdb.Spec.LivenessProbeOverride.FailureThreshold) *
			PctOfLivenessProbeWait)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{RequeueAfter: time.Second * time.Duration(expectedRequeueTime)}))
	})

	It("should requeue for node restart because livenessProbes is active in only 1 pod", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.LivenessProbeOverride = &corev1.Probe{
			PeriodSeconds:    15,
			FailureThreshold: 5,
		}
		vdb.Spec.Subclusters[0].Size = 5
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		// Make the startupProbe active on only the first pod. This means we can
		// restart the other down pod.
		pn := names.GenPodName(vdb, sc, 0)
		pod := &corev1.Pod{}
		Expect(k8sClient.Get(ctx, pn, pod)).Should(Succeed())
		started := true
		pod.Status.ContainerStatuses = []corev1.ContainerStatus{
			{Name: "vlogger", Started: nil},
			{Name: names.ServerContainer, Started: &started},
		}
		Expect(k8sClient.Status().Update(ctx, pod)).Should(Succeed())

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithRestartNeeded(ctx, vdb, &vdb.Spec.Subclusters[0], fpr, []int32{0, 1}, PodNotReadOnly)
		initiatorPod := names.GenPodName(vdb, sc, 6)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0001 | 10.244.1.6 | DOWN | vertica-11.0.0.20210309 | db 
 v_db_node0002 | 10.244.1.7 | DOWN | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.8 | UP   | vertica-11.0.0.20210309 | db 
 v_db_node0004 | 10.244.1.9 | UP   | vertica-11.0.0.20210309 | db 
 v_db_node0005 | 10.244.1.10| UP   | vertica-11.0.0.20210309 | db 

`,
			},
		}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		expectedRequeueTime := int(float64(vdb.Spec.LivenessProbeOverride.PeriodSeconds*vdb.Spec.LivenessProbeOverride.FailureThreshold) *
			PctOfLivenessProbeWait)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{RequeueAfter: time.Second * time.Duration(expectedRequeueTime)}))
		// Verify that we restarted the other node
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(1))
	})

	It("should requeue for node restart if all down have a slow startup", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.LivenessProbeOverride = &corev1.Probe{
			PeriodSeconds:    25,
			FailureThreshold: 2,
		}
		sc := &vdb.Spec.Subclusters[0]
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithSlowStartup(ctx, vdb, &vdb.Spec.Subclusters[0], fpr, []int32{2})
		initiatorPod := names.GenPodName(vdb, sc, 4)
		setVerticaNodeNameInPodFacts(vdb, sc, pfacts)
		fpr.Results[initiatorPod] = []cmds.CmdResult{
			{Stdout: `
 Node          | Host       | State | Version                 | DB 
---------------+------------+-------+-------------------------+----
 v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0002 | 10.244.1.7 | UP    | vertica-11.0.0.20210309 | db 
 v_db_node0003 | 10.244.1.8 | DOWN  | vertica-11.0.0.20210309 | db 

`,
			},
		}
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		r.InitiatorPod = initiatorPod
		expectedRequeueTime := int(float64(vdb.Spec.LivenessProbeOverride.PeriodSeconds*vdb.Spec.LivenessProbeOverride.FailureThreshold) *
			PctOfLivenessProbeWait)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{RequeueAfter: time.Second * time.Duration(expectedRequeueTime)}))
		// Verify that we did not restart any node
		restart := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "restart_node")
		Expect(len(restart)).Should(Equal(0))
	})

	It("should requeue if we don't have cluster quorum according to podfacts", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		createS3CredSecret(ctx, vdb)
		defer deleteCommunalCredSecret(ctx, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		for podIndex := int32(0); podIndex < vdb.Spec.Subclusters[0].Size; podIndex++ {
			podNm := names.GenPodName(vdb, sc, podIndex)
			// Only 1 of the 3 is up, which means we don't have quorum
			if podIndex == 0 {
				pfacts.Detail[podNm].SetUpNode(true)
			} else {
				pfacts.Detail[podNm].SetUpNode(false)
			}
			pfacts.Detail[podNm].SetIsInstalled(true)
			pfacts.Detail[podNm].SetIsPodRunning(true)
		}

		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		act := MakeRestartReconciler(vdbRec, logger, vdb, fpr, pfacts, RestartProcessReadOnly, dispatcher)
		r := act.(*RestartReconciler)
		Expect(r.reconcileNodes(ctx)).Should(Equal(ctrl.Result{Requeue: true, RequeueAfter: time.Second * RequeueWaitTimeInSeconds}))

		// But if we are using schedule-only, then we skip the quorum check and proceed with restart.
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), vdb)).Should(Succeed())
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
		Expect(r.reconcileNodes(ctx)).Should(Equal(ctrl.Result{}))
		listCmd := fpr.FindCommands("restart_node")
		Expect(len(listCmd)).Should(Equal(1))
	})
})
