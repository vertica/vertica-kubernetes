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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/atconf"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

var _ = Describe("k8s/install_reconcile_test", func() {
	ctx := context.Background()

	It("should detect no install is needed", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, true)
		defer test.DeletePods(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{}
		pfact := MakePodFacts(k8sClient, fpr)
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		for i := int32(0); i < 3; i++ {
			Expect(drecon.PFacts.Detail[names.GenPodName(vdb, sc, i)].isInstalled.IsTrue()).Should(BeTrue(), fmt.Sprintf("Pod index %d", i))
		}
	})

	It("should detect one pod that needs to be installed", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, 0): []cmds.CmdResult{{}},
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{Stderr: "cat: " + vdb.GenInstallerIndicatorFileName() + ": No such file or directory",
					Err: errors.New("command terminated with exit code 1")},
			},
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{{}},
		}}

		pfact := MakePodFacts(k8sClient, fpr)
		Expect(pfact.Collect(ctx, vdb)).Should(Succeed())
		pfact.Detail[names.GenPodName(vdb, sc, 1)].dbExists = tristate.False
		// Reset the pod runner output to dump the compat21 node number
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{}, // remove old config
				{}, // Debug info for admintools.conf after admintools.conf update
				{}, // Copy admintools.conf to the pod
				{Stdout: "node0003 = 192.168.0.1,/d,/d\n"}}, // Get of compat21 node name
		}
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		drecon.ATWriter = &atconf.FakeWriter{}
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(drecon.PFacts.Detail[names.GenPodName(vdb, sc, 1)].isInstalled.IsFalse()).Should(BeTrue())
		Expect(fpr.Histories[len(fpr.Histories)-1]).Should(Equal(
			cmds.CmdHistory{Pod: names.GenPodName(vdb, sc, 1), Command: drecon.genCmdCreateInstallIndicator("node0003")}))
	})

	It("should try install if a pod has not run the installer yet", func() {
		vdb := vapi.MakeVDB()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, 0): []cmds.CmdResult{{}},
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{{
				Stderr: "cat: " + vdb.GenInstallerIndicatorFileName() + ": No such file or directory",
				Err:    errors.New("command terminated with exit code 1")}},
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{{
				Stderr: "cat: " + vdb.GenInstallerIndicatorFileName() + ": No such file or directory",
				Err:    errors.New("command terminated with exit code 1")}},
		}}

		pfact := MakePodFacts(k8sClient, fpr)
		Expect(pfact.Collect(ctx, vdb)).Should(Succeed())
		pfact.Detail[names.GenPodName(vdb, sc, 1)].dbExists = tristate.False
		pfact.Detail[names.GenPodName(vdb, sc, 2)].dbExists = tristate.False
		// Reset the pod runner output to dump the compat21 node number
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{}, // Remove old admintools.conf
				{}, // Debug info for admintools.conf after updating admintools.conf
				{}, // Copy admintools.conf to the pod
				{Stdout: "node0003 = 192.168.0.1,/d,/d\n"}}, // Get of compat21 node name
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{
				{}, // Remove old admintools.conf
				{}, // Debug info for admintools.conf after updating admintools.conf
				{}, // Copy admintools.conf to the pod
				{Stdout: "node0003 = 192.168.0.2,/d,/d\n"}}, // Get of compat21 node name
		}
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		drecon.ATWriter = &atconf.FakeWriter{}
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		cmdHist := fpr.FindCommands(fmt.Sprintf("cat > %s", paths.AdminToolsConf))
		Expect(len(cmdHist)).Should(Equal(3))
		// We should see two instances of creating the install indicator -- one at each host that we install at
		cmdHist = fpr.FindCommands(drecon.genCmdCreateInstallIndicator("node0003")...)
		Expect(len(cmdHist)).Should(Equal(2))
	})

	It("should skip call exec on a pod if is not yet running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
		pfact := MakePodFacts(k8sClient, fpr)
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		drecon.ATWriter = &atconf.FakeWriter{}
		res, err := drecon.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("try install when not all pods are running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := &vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		// Make only pod -1 runable.
		const PodIndex = 1
		test.SetPodStatus(ctx, k8sClient, 1 /* funcOffset */, names.GenPodName(vdb, sc, 1), ScIndex, PodIndex, test.AllPodsRunning)

		fpr := &cmds.FakePodRunner{}
		pfact := MakePodFacts(k8sClient, fpr)
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		res, err := drecon.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
	})

	It("install should accept eula", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := &vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfact := createPodFactsWithInstallNeeded(ctx, vdb, fpr)
		actor := MakeInstallReconciler(vdbRec, logger, vdb, fpr, pfact)
		drecon := actor.(*InstallReconciler)
		res, err := drecon.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		cmds := fpr.FindCommands(paths.EulaAcceptanceScript)
		Expect(len(cmds)).Should(Equal(4)) // 2 for each pod; 1 to copy and 1 to execute the script
	})
})
