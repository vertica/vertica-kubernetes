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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("drainnode_reconcile", func() {
	ctx := context.Background()

	It("should query sessions if pod is pending delete", func() {
		const origSize = 2
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: origSize},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		vdb.Spec.Subclusters[0].Size-- // Reduce size to make one pod pending delete
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		cmds := fpr.FindCommands("select count(*) from session")
		Expect(len(cmds)).Should(Equal(1))
	})

	It("should not query sessions if no pod is pending delete", func() {
		const origSize = 2
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: origSize},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &testPassword)
		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		cmds := fpr.FindCommands("select count(*) from session")
		Expect(len(cmds)).Should(Equal(0))
	})

	It("should requeue if one pending delete pod has active connections", func() {
		const origSize = 2
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Size: origSize},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		vdb.Spec.Subclusters[0].Size-- // Reduce size to make one pod pending delete
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		penDelPodName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 1)
		fpr.Results[penDelPodName] = []cmds.CmdResult{
			{Stdout: "10\n"},
		}

		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{RequeueAfter: 1 * time.Second}))
		cmds := fpr.FindCommands("select count(*) from session")
		Expect(len(cmds)).Should(Equal(1))
	})

	It("should set drain-start annotation if not present and timeout > 0", func() {
		const origSize = 3
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.ActiveConnectionsDrainSecondsAnnotation] = "10"

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		vdb.Spec.Subclusters[0].Size -= 2 // Reduce size to make two pods pending delete
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Results[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 1)] = []cmds.CmdResult{
			{Stdout: "10\n"},
		}
		fpr.Results[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 2)] = []cmds.CmdResult{
			{Stdout: "10\n"},
		}
		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)

		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.RequeueAfter).Should(Equal(1 * time.Second))
		cmds := fpr.FindCommands("select count(*) from session")
		Expect(len(cmds)).Should(Equal(2))

		// Check annotation was added
		Expect(vdb.Annotations).To(HaveKey(vmeta.DrainStartAnnotation))
	})

	It("should return nil if timeout has expired", func() {
		const origSize = 3
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.ActiveConnectionsDrainSecondsAnnotation] = "1"
		vdb.Annotations[vmeta.DrainStartAnnotation] = time.Now().Add(-2 * time.Second).Format(time.RFC3339)

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		vdb.Spec.Subclusters[0].Size-- // Reduce size to make one pod pending delete
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Results[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 2)] = []cmds.CmdResult{
			{Stdout: "10\n"},
		}
		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)

		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))

		cmds := fpr.FindCommands("select count(*) from session")
		Expect(len(cmds)).Should(Equal(1))
	})

	It("should remove drain-start annotation if no pods are pending delete", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.ActiveConnectionsDrainSecondsAnnotation] = "10"
		vdb.Annotations[vmeta.DrainStartAnnotation] = time.Now().Format(time.RFC3339)

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)

		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		Expect(vdb.Annotations).NotTo(HaveKey(vmeta.DrainStartAnnotation))
	})

	It("should return immediately if timeout is zero", func() {
		const origSize = 3
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.ActiveConnectionsDrainSecondsAnnotation] = "0"

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		// Restore original size prior to deletion to ensure all pods are cleaned up
		defer func() { vdb.Spec.Subclusters[0].Size = origSize; test.DeletePods(ctx, k8sClient, vdb) }()
		vdb.Spec.Subclusters[0].Size-- // Reduce size to make one pod pending delete
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{Results: make(cmds.CmdResults)}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		fpr.Results[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 2)] = []cmds.CmdResult{
			{Stdout: "10\n"},
		}

		r := MakeDrainNodeReconciler(vdbRec, vdb, fpr, pfacts)

		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
	})
})
