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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("sandboxshutdown_reconciler", func() {
	ctx := context.Background()
	const maincluster = "main"
	const subcluster1 = "sc1"
	const sandbox1 = "sandbox1"
	const tID = "12345"
	It("should update configmap", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Shutdown: true, Type: vapi.SecondarySubcluster},
		}

		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: maincluster},
			{Name: subcluster1, Shutdown: false},
		}
		test.CreateConfigMap(ctx, k8sClient, vdb, vmeta.SandboxControllerShutdownTriggerID, tID, sandbox1)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sandbox1)

		validateShutdownReconcile(ctx, vdb, false)
		cm := &corev1.ConfigMap{}
		nm := names.GenSandboxConfigMapName(vdb, sandbox1)
		Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]).ShouldNot(Equal(""))
		Expect(cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]).ShouldNot(Equal(tID))

		id := cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]
		vdb.Spec.Subclusters[1].Shutdown = false
		vdb.Status.Subclusters[1].Shutdown = true
		validateShutdownReconcile(ctx, vdb, false)
		Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]).ShouldNot(Equal(""))
		Expect(cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]).ShouldNot(Equal(id))

		id = cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]
		vdb.Spec.Subclusters[1].Shutdown = true
		validateShutdownReconcile(ctx, vdb, false)
		Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerShutdownTriggerID]).Should(Equal(id))
	})
})

func validateShutdownReconcile(ctx context.Context, vdb *vapi.VerticaDB, requeue bool) {
	r := MakeSandboxShutdownReconciler(vdbRec, logger, vdb)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(err).Should(Succeed())
	Expect(res).Should(Equal(ctrl.Result{Requeue: requeue}))
}
