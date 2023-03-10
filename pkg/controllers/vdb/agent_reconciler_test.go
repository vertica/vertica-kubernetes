/*
 (c) Copyright [2021-2023] Micro Focus or one of its affiliates.
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("agent_reconcile", func() {
	ctx := context.Background()

	It("should start the agent if it isn't running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Annotations[vapi.StartAgent] = vapi.AgentEnabled
		vdb.Spec.Subclusters[0].Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindVerticaAgentStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(2))
	})

	It("should avoid starting the agent if vdb.spec.annotations[start-agent] is not set to 'yes'", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindVerticaAgentStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(0))
	})

	It("should avoid starting the agent if DB is not included", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Annotations[vapi.StartAgent] = vapi.AgentEnabled
		vdb.Spec.Subclusters[0].Size = 1
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithAgentNotRunning(ctx, vdb, fpr)
		pfacts.Detail[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)].dbExists = false
		r := MakeAgentReconciler(vdbRec, logger, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		cmds := fpr.FindCommands("/opt/vertica/sbin/vertica_agent", "start")
		Expect(len(cmds)).Should(Equal(0))
	})
})

func reconcileAndFindVerticaAgentStart(ctx context.Context, vdb *vapi.VerticaDB) []cmds.CmdHistory {
	fpr := &cmds.FakePodRunner{}
	pfacts := createPodFactsWithAgentNotRunning(ctx, vdb, fpr)
	r := MakeAgentReconciler(vdbRec, logger, vdb, fpr, pfacts)
	Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	return fpr.FindCommands("/opt/vertica/sbin/vertica_agent", "start")
}
