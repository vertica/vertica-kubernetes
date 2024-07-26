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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("unsandboxsubcluster_reconcile", func() {
	ctx := context.Background()
	maincluster := "main"
	subcluster1 := "sc1"
	sandbox1 := "sandbox1"

	It("should exit without error if not using an EON database", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.ShardCount = 0 // Force enterprise database
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		Expect(vdb.IsEON()).Should(BeFalse())
		r := MakeUnsandboxSubclusterReconciler(vdbRec, logger, vdb, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit without error if using schedule-only policy", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeUnsandboxSubclusterReconciler(vdbRec, logger, vdb, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should update sandbox config maps correctly", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		nm := names.GenSandboxConfigMapName(vdb, sandbox1)
		cm := builder.BuildSandboxConfigMap(nm, vdb, sandbox1)
		Expect(k8sClient.Create(ctx, cm)).Should(Succeed())
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sandbox1)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		r := MakeUnsandboxSubclusterReconciler(vdbRec, logger, vdb, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// should update the config map with unsandbox trigger ID
		newCM, res, err := getConfigMap(ctx, vdbRec, vdb, nm)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(newCM.Data[vapi.SandboxNameKey]).Should(Equal(sandbox1))
		Expect(newCM.Annotations[vmeta.SandboxControllerUnsandboxTriggerID]).ShouldNot(BeEmpty())
	})
})
