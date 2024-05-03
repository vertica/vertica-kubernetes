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
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("sandboxupgrade_reconciler", func() {
	ctx := context.Background()
	const maincluster = "main"
	const subcluster1 = "sc1"
	const sandbox1 = "sandbox1"
	const newImage = "vertica-k8s:newimage"
	const tID = "12345"
	It("should update configmap", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}

		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Image: newImage, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateConfigMap(ctx, k8sClient, vdb, tID, sandbox1)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sandbox1)

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts)).Should(Succeed())
		Expect(vk8s.GetServerImage(sts.Spec.Template.Spec.Containers)).ShouldNot(Equal(newImage))

		validateReconcile(ctx, vdb, false)
		cm := &corev1.ConfigMap{}
		nm := names.GenConfigMapName(vdb, sandbox1)
		Expect(k8sClient.Get(ctx, nm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerTriggerID]).ShouldNot(Equal(""))
		Expect(cm.Annotations[vmeta.SandboxControllerTriggerID]).ShouldNot(Equal(tID))
	})

	It("should exit without error", func() {
		vdb := vapi.MakeVDBForVclusterOps()

		// there is no sandbox in spec
		validateReconcile(ctx, vdb, false)

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}

		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
		}

		// no sandbox in status
		validateReconcile(ctx, vdb, true)

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateConfigMap(ctx, k8sClient, vdb, tID, sandbox1)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sandbox1)
		vdb.Spec.Sandboxes[0].Image = vdb.Spec.Image
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}

		// image has not changed
		validateReconcile(ctx, vdb, false)

	})

	It("should fail if configmap does not exist", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}

		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Image: newImage, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		r := MakeSandboxUpgradeReconciler(vdbRec, logger, vdb)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
	})
})

func validateReconcile(ctx context.Context, vdb *vapi.VerticaDB, requeue bool) {
	r := MakeSandboxUpgradeReconciler(vdbRec, logger, vdb)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(err).Should(Succeed())
	Expect(res).Should(Equal(ctrl.Result{Requeue: requeue}))
}
