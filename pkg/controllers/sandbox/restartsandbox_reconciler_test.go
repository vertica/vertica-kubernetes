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

package sandbox

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("restartsandbox_reconcile", func() {
	ctx := context.Background()
	maincluster := "main"
	subcluster1 := "sc1"
	sandbox1 := "sandbox1"

	It("should reconcile based on shutdown state", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster, Shutdown: true},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Shutdown: true, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		pfacts.NeedCollection = false
		pfacts.SandboxName = sandbox1
		rec := MakeRestartSandboxReconciler(sbRec, vdb, &pfacts, logger)
		r := rec.(*RestartSandboxReconciler)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))

		vdb.Spec.Sandboxes[0].Shutdown = false
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Shutdown: false, Subclusters: []string{subcluster1}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		rec = MakeRestartSandboxReconciler(sbRec, vdb, &pfacts, logger)
		r = rec.(*RestartSandboxReconciler)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.Subclusters[1].Shutdown).Should(BeFalse())

		vdb.Status.Sandboxes[0].Shutdown = true
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		rec = MakeRestartSandboxReconciler(sbRec, vdb, &pfacts, logger)
		r = rec.(*RestartSandboxReconciler)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	})
})
