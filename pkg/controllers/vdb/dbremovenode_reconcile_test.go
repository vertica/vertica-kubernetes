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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("dbremovenode_reconcile", func() {
	ctx := context.Background()

	It("dbremovenode should not return an error if the sts doesn't exist", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		recon := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should call db_remove_node on one pod", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		uninstallPod := builder.BuildPod(vdb, sc, 1)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		actor := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		recon := actor.(*DBRemoveNodeReconciler)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Histories = make([]cmds.CmdHistory, 0) // reset the calls so the first one is admintools
		_, err := recon.removeNodesInSubcluster(ctx, sc, 1, 1)
		Expect(err).Should(Succeed())
		Expect(fpr.Histories[0].Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"db_remove_node",
			"--hosts="+uninstallPod.Spec.Hostname+"."+uninstallPod.Spec.Subdomain,
		))
	})

	It("should call db_remove_node on multiple pods", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)
		sc.Size = 1 // mimic a pending db_remove_node

		uninstallPods := []types.NamespacedName{names.GenPodName(vdb, sc, 1), names.GenPodName(vdb, sc, 2)}

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).Should(ContainElements(
			"/opt/vertica/bin/admintools",
			"db_remove_node",
			"--hosts="+pfacts.Detail[uninstallPods[0]].dnsName+","+pfacts.Detail[uninstallPods[1]].dnsName,
		))
	})

	It("should skip remove node and requeue because there aren't any pods running", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)
		sc.Size = sc.Size - 1 // mimic a pending db_remove_node

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
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
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdbCopy)
		sc.Size = 2 // Set to 2 to mimic a pending uninstall of the last pod

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		removePod := names.GenPodName(vdb, sc, 2)
		pfacts.Detail[removePod].dbExists = tristate.False
		r := MakeDBRemoveNodeReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		Expect(len(fpr.Histories)).Should(BeNumerically(">", 0))
		lastCall := fpr.Histories[len(fpr.Histories)-1]
		Expect(lastCall.Command).ShouldNot(ContainElements("/opt/vertica/bin/admintools", "db_remove_node"))
	})
})
