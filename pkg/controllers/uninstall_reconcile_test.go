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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("k8s/uninstall_reconcile", func() {
	ctx := context.Background()

	It("reconcile subcluster should not return an error if the sts doesn't exist", func() {
		vdb := vapi.MakeVDB()

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		recon := MakeUninstallReconciler(vrec, logger, vdb, fpr, &pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should uninstall one pod", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		uninstallPod := buildPod(vdb, sc, 1)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		actor := MakeUninstallReconciler(vrec, logger, vdb, fpr, &pfacts)
		recon := actor.(*UninstallReconciler)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Histories = make([]cmds.CmdHistory, 0) // reset the calls so the first one is update_vertica
		_, err := recon.uninstallPodsInSubcluster(ctx, sc, 1, 1)
		Expect(err).Should(Succeed())
		Expect(fpr.Histories[0].Command).Should(ContainElements(
			"/opt/vertica/sbin/update_vertica",
			"--remove-hosts",
			uninstallPod.Spec.Hostname+"."+uninstallPod.Spec.Subdomain,
		))
	})

	It("should skip uninstall and requeue because there aren't any pods running", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 2
		vdbCopy := vdb.DeepCopy() // Take a copy so that cleanup with original size
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdbCopy)
		sc.Size = 1 // Set to 1 to mimic a pending uninstall

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeUninstallReconciler(vrec, logger, vdb, fpr, &pfacts)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should call uninstall for multiple pods", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 3
		vdbCopy := vdb.DeepCopy() // Take a copy so that we cleanup with the original size
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdbCopy)
		sc.Size = 1 // mimic a pending db_remove_node

		uninstallPods := []types.NamespacedName{names.GenPodName(vdb, sc, 1), names.GenPodName(vdb, sc, 2)}

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeUninstallReconciler(vrec, logger, vdb, fpr, &pfacts)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		updateVerticaCall := fpr.FindCommands("/opt/vertica/sbin/update_vertica")
		Expect(len(updateVerticaCall)).Should(Equal(1))
		Expect(updateVerticaCall[0].Command).Should(ContainElements(
			"/opt/vertica/sbin/update_vertica",
			"--remove-hosts",
			pfacts.Detail[uninstallPods[0]].dnsName+","+pfacts.Detail[uninstallPods[1]].dnsName,
		))
	})

})
