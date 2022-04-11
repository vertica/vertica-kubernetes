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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("rebalanceshards_reconcile", func() {
	ctx := context.Background()

	It("should rebalance shards if one pod doesn't have any subscriptions", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1},
			{Name: "sc2", Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfn := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pfn].upNode = true
		pfacts.Detail[pfn].shardSubscriptions = 0
		pfn = names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
		pfacts.Detail[pfn].shardSubscriptions = 3
		pfacts.Detail[pfn].upNode = true
		r := MakeRebalanceShardsReconciler(vdbRec, logger, vdb, fpr, &pfacts, "")
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		atCmd := fpr.FindCommands("select rebalance_shards('sc1')")
		Expect(len(atCmd)).Should(Equal(1))
		atCmd = fpr.FindCommands("select rebalance_shards('sc2')")
		Expect(len(atCmd)).Should(Equal(0))
	})

	It("should only run rebalance shards against specified subcluster ", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: 1},
			{Name: "sc2", Size: 1},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		for i := range vdb.Spec.Subclusters {
			pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[i], 0)
			pfacts.Detail[pn].upNode = true
			pfacts.Detail[pn].shardSubscriptions = 0
		}
		r := MakeRebalanceShardsReconciler(vdbRec, logger, vdb, fpr, &pfacts, vdb.Spec.Subclusters[1].Name)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		atCmd := fpr.FindCommands("select rebalance_shards('sc1')")
		Expect(len(atCmd)).Should(Equal(0))
		atCmd = fpr.FindCommands("select rebalance_shards('sc2')")
		Expect(len(atCmd)).Should(Equal(1))
	})
})
