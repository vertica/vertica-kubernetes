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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/license"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("k8s/install_reconcile_test", func() {
	ctx := context.Background()

	It("should detect no install is needed", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, true)
		defer deletePods(ctx, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{}
		pfact := MakePodFacts(k8sClient, fpr)
		actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		for i := int32(0); i < 3; i++ {
			Expect(drecon.PFacts.Detail[names.GenPodName(vdb, sc, i)].isInstalled.IsTrue()).Should(BeTrue(), fmt.Sprintf("Pod index %d", i))
		}
	})

	It("should detect one pod that needs to be installed", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, 0): []cmds.CmdResult{{}},
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{Stderr: "cat: " + paths.GenInstallerIndicatorFileName(vdb) + ": No such file or directory",
					Err: errors.New("command terminated with exit code 1")},
			},
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{{}},
		}}

		pfact := MakePodFacts(k8sClient, fpr)
		Expect(pfact.Collect(ctx, vdb)).Should(Succeed())
		// Reset the pod runner output to dump the compat21 node number
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{}, // Check for stale admintools.conf
				{Stdout: "node0003 = 192.168.0.1,/d,/d\n"}}, // Get of compat21 node name
		}
		actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(drecon.PFacts.Detail[names.GenPodName(vdb, sc, 1)].isInstalled.IsFalse()).Should(BeTrue())
		Expect(fpr.Histories[len(fpr.Histories)-1]).Should(Equal(
			cmds.CmdHistory{Pod: names.GenPodName(vdb, sc, 1), Command: drecon.genCmdCreateInstallIndicator("node0003")}))
	})

	It("should call update_vertica if a pod has not run the installer yet", func() {
		vdb := vapi.MakeVDB()
		createPods(ctx, vdb, AllPodsRunning)
		defer deletePods(ctx, vdb)

		sc := &vdb.Spec.Subclusters[0]
		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, 0): []cmds.CmdResult{{}},
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{{
				Stderr: "cat: " + paths.GenInstallerIndicatorFileName(vdb) + ": No such file or directory",
				Err:    errors.New("command terminated with exit code 1")}},
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{{
				Stderr: "cat: " + paths.GenInstallerIndicatorFileName(vdb) + ": No such file or directory",
				Err:    errors.New("command terminated with exit code 1")}},
		}}

		pfact := MakePodFacts(k8sClient, fpr)
		Expect(pfact.Collect(ctx, vdb)).Should(Succeed())
		// Reset the pod runner output to dump the compat21 node number
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, 1): []cmds.CmdResult{
				{}, // Check for stale admintools.conf
				{Stdout: "node0003 = 192.168.0.1,/d,/d\n"}}, // Get of compat21 node name
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{
				{}, // Check for stale admintools.conf
				{Stdout: "node0003 = 192.168.0.2,/d,/d\n"}}, // Get of compat21 node name
		}
		actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		Expect(drecon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		cmdHist := fpr.FindCommands("/opt/vertica/sbin/update_vertica")
		Expect(len(cmdHist)).Should(Equal(1))
		updateVerticaCall := cmdHist[0]
		hlSvcName := names.GenHlSvcName(vdb).Name
		expHostListRegex := fmt.Sprintf("[a-z0-9-]+..%s,[a-z0-9-]+..%s", hlSvcName, hlSvcName)
		Expect(updateVerticaCall.Command).Should(ContainElement(MatchRegexp(expHostListRegex)))
		Expect(updateVerticaCall.Pod).Should(Equal(names.GenPodName(vdb, sc, 0)))
		// We should see two instances of creating the install indicator -- one at each host that we install at
		cmdHist = fpr.FindCommands(drecon.genCmdCreateInstallIndicator("node0003")...)
		Expect(len(cmdHist)).Should(Equal(2))
	})

	It("should skip call exec on a pod if is not yet running", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)

		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
		pfact := MakePodFacts(k8sClient, fpr)
		actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		res, err := drecon.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should call update_vertica from the one runable pod", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := &vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		createPods(ctx, vdb, AllPodsNotRunning)
		defer deletePods(ctx, vdb)
		// Make only pod -1 runable.
		const PodIndex = 1
		setPodStatus(ctx, 1 /* funcOffset */, names.GenPodName(vdb, sc, 1), ScIndex, PodIndex, AllPodsRunning)

		fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{
			names.GenPodName(vdb, sc, PodIndex): []cmds.CmdResult{{
				Stderr: "cat: " + paths.GenInstallerIndicatorFileName(vdb) + ": No such file or directory",
				Err:    errors.New("command terminated with exit code 1")}},
		}}
		pfact := MakePodFacts(k8sClient, fpr)
		Expect(pfact.Collect(ctx, vdb)).Should(Succeed())
		// Reset the pod runner output to dump the compat21 node number
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, PodIndex): []cmds.CmdResult{
				{}, // Check for stale admintools.conf
				{}, // Dump admintools.conf
				{}, // run update_vertica
				{}, // Dump admintools.conf
				{Stdout: "node0003 = 192.168.0.1,/d,/d\n"}}, // Get of compat21 node name
		}
		actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
		drecon := actor.(*InstallReconciler)
		res, err := drecon.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeTrue())
		Expect(len(fpr.Histories)).Should(BeNumerically(">=", 3))
		Expect(fpr.Histories[0].Pod).Should(Equal(names.GenPodName(vdb, sc, PodIndex)))
		Expect(fpr.Histories[1].Pod).Should(Equal(names.GenPodName(vdb, sc, PodIndex)))
		Expect(fpr.Histories[2].Pod).Should(Equal(names.GenPodName(vdb, sc, PodIndex)))
	})

	It("Should all have ipv6 addresses if pods created with ipv6", func() {
		podList, verticaUpdateCmd := createInstallPodsHelper(ctx, true)
		Expect(verticaUpdateCmd).Should(ContainElement("--ipv6"))
		Expect(podsAllHaveIPv6(podList)).Should(BeTrue())
	})

	It("Should not have ipv6 addresses if pods created with ipv4", func() {
		podList, verticaUpdateCmd := createInstallPodsHelper(ctx, false)
		Expect(verticaUpdateCmd).ShouldNot(ContainElement("--ipv6"))
		Expect(podsAllHaveIPv6(podList)).Should(BeFalse())
	})
})

func createInstallPodsHelper(ctx context.Context, ipv6 bool) (podList []*PodFact, verticaUpdateCmd []string) {
	const clusterSize = 3
	vdb := vapi.MakeVDB()
	vdb.Spec.Subclusters[0].Size = clusterSize
	vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{Name: "secondary", Size: 2})
	if ipv6 {
		createIPv6Pods(ctx, vdb, AllPodsRunning)
	} else {
		createPods(ctx, vdb, AllPodsRunning)
	}
	defer deletePods(ctx, vdb)

	podList = make([]*PodFact, 0, clusterSize)
	fpr := &cmds.FakePodRunner{}
	pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, 1)
	for _, v := range pfacts.Detail {
		podList = append(podList, v)
	}

	pfact := MakePodFacts(k8sClient, fpr)
	actor := MakeInstallReconciler(vrec, logger, vdb, fpr, &pfact)
	drecon := actor.(*InstallReconciler)
	licensePath, _ := license.GetPath(ctx, k8sClient, drecon.Vdb)
	verticaUpdateCmd = drecon.genCmdInstall(podList, licensePath)

	return podList, verticaUpdateCmd
}
