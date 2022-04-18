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
	"errors"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("dbaddnode_reconcile", func() {
	ctx := context.Background()

	It("should not call db_add_node if db already exists everywhere", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).ShouldNot(ContainElements("/opt/vertica/bin/admintools", "db_add_node"))
	})

	It("should not call db_add_node if no db exists anywhere", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 3)
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).ShouldNot(ContainElements("/opt/vertica/bin/admintools", "db_add_node"))
	})

	It("should call db_add_node if db exists but is missing at one running pod", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		atCmd := fpr.FindCommands("db_add_node")
		Expect(len(atCmd)).Should(Equal(1))
		Expect(atCmd[0].Command).Should(ContainElements("/opt/vertica/bin/admintools", "db_add_node"))
	})

	It("should succeed if we try to add a node and hit the limit", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		// Make a specific pod as not having a db.
		podWithNoDB := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 1)
		pfacts.Detail[podWithNoDB].dbExists = tristate.False
		pfacts.Detail[podWithNoDB].upNode = false
		// The pod we run db_add_node is the other pod. We setup its pod runner
		// so that it fails because we hit the node limit.
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results[atPod] = []cmds.CmdResult{
			{}, // Dump admintools.conf
			{
				Err: errors.New("admintools command failed"),
				Stdout: "There was an error adding the nodes to the database: DB client operation \"create nodes\" failed during `ddl`: " +
					"Severity: ROLLBACK, Message: Cannot create another node. The current license permits 3 node(s) and the database catalog " +
					"already contains 3 node(s), Sqlstate: V2001",
			},
		}
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node")
		Expect(len(lastCall)).Should(Equal(1))
	})

	It("should rebalance shards if we scale out", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		atCmd := fpr.FindCommands("select rebalance_shards('defaultsubcluster')")
		Expect(len(atCmd)).Should(Equal(0))
	})

	It("should not call select rebalance_shards() if no node has been added", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		atCmd := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node")
		Expect(len(atCmd)).Should(Equal(0))
		atCmd = fpr.FindCommands("select rebalance_shards('defaultsubcluster')")
		Expect(len(atCmd)).Should(Equal(0))
	})

	It("should not add node and requeue if one pod is missing db and another pod isn't running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		// Make a specific pod as not having a db.
		podWithNoDB := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 1)
		pfacts.Detail[podWithNoDB].dbExists = tristate.False
		pfacts.Detail[podWithNoDB].upNode = false
		// Make a specific pod as having an unknown state
		podInUnknownState := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 2)
		pfacts.Detail[podInUnknownState].dbExists = tristate.None
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node")
		Expect(len(lastCall)).Should(Equal(0))
	})

	It("should have a single add node call if multi pods are missing db", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 2)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		r := MakeDBAddNodeReconciler(vdbRec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		lastCall := fpr.FindCommands("/opt/vertica/bin/admintools", "-t", "db_add_node")
		Expect(len(lastCall)).Should(Equal(1))
	})
})
