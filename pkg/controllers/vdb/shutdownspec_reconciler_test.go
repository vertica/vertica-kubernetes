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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("shutdownspec_reconciler", func() {
	ctx := context.Background()
	maincluster := "main"
	subcluster1 := "sc1"
	subcluster2 := "sc2"
	sandbox1 := "sandbox1"

	It("should reconcile based on sandbox shutdown field", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Shutdown: true, Subclusters: []vapi.SandboxSubcluster{
				{Name: subcluster1}, {Name: subcluster2, Type: vapi.SecondarySubcluster}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1, subcluster2}},
		}

		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: maincluster, Type: vapi.PrimarySubcluster, Detail: []vapi.VerticaDBPodStatus{}},
			{Name: subcluster1, Type: vapi.SecondarySubcluster, Detail: []vapi.VerticaDBPodStatus{}},
			{Name: subcluster2, Type: vapi.SecondarySubcluster, Detail: []vapi.VerticaDBPodStatus{}},
		}

		Ω(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		r := MakeShutdownSpecReconciler(vdbRec, vdb, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))
		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		Expect(vmeta.IsShutdownDrivenBySandbox(newVdb.Spec.Subclusters[1].Annotations)).Should(BeTrue())
		Expect(vmeta.IsShutdownDrivenBySandbox(newVdb.Spec.Subclusters[2].Annotations)).Should(BeTrue())
		Expect(newVdb.Spec.Subclusters[1].Shutdown).Should(BeTrue())
		Expect(newVdb.Spec.Subclusters[2].Shutdown).Should(BeTrue())

		newVdb.Spec.Sandboxes[0].Shutdown = false
		Expect(k8sClient.Update(ctx, newVdb)).Should(Succeed())
		r = MakeShutdownSpecReconciler(vdbRec, newVdb, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))
		newVdb2 := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb2)).Should(Succeed())
		Expect(vmeta.IsShutdownDrivenBySandbox(newVdb2.Spec.Subclusters[1].Annotations)).Should(BeFalse())
		Expect(vmeta.IsShutdownDrivenBySandbox(newVdb2.Spec.Subclusters[2].Annotations)).Should(BeFalse())
		Expect(newVdb2.Spec.Subclusters[1].Shutdown).Should(BeFalse())
		Expect(newVdb.Spec.Subclusters[2].Shutdown).Should(BeFalse())

		newVdb2.Spec.Shutdown = true
		newVdb2.Spec.Subclusters[0].Shutdown = false
		Expect(k8sClient.Update(ctx, newVdb2)).Should(Succeed())
		r = MakeShutdownSpecReconciler(vdbRec, newVdb2, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))
		newVdb3 := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb3)).Should(Succeed())
		Expect(vmeta.IsShutdownDrivenByMain(newVdb3.Spec.Subclusters[0].Annotations)).Should(BeTrue())
		Expect(newVdb3.Spec.Subclusters[0].Shutdown).Should(BeTrue())

		newVdb3.Spec.Shutdown = false
		Expect(k8sClient.Update(ctx, newVdb3)).Should(Succeed())
		r = MakeShutdownSpecReconciler(vdbRec, newVdb3, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(BeNil())
		Expect(res).Should(Equal(ctrl.Result{}))
		newVdb4 := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb4)).Should(Succeed())
		Expect(vmeta.IsShutdownDrivenByMain(newVdb4.Spec.Subclusters[0].Annotations)).Should(BeFalse())
		Expect(newVdb4.Spec.Subclusters[0].Shutdown).Should(BeFalse())
	})
})
