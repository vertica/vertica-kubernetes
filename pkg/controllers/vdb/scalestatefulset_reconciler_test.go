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
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("scalestatefulset_reconciler", func() {
	ctx := context.Background()

	It("should scale the sandbox sts to zero if subcluster is shut down", func() {
		vdb := v1.MakeVDB()
		const sc1 = "sc1"
		vdb.Spec.Subclusters = []v1.Subcluster{
			{Name: sc1, Size: 3, Shutdown: false},
		}
		vdb.Spec.Sandboxes = []v1.Sandbox{
			{Name: sc1, Subclusters: []v1.SandboxSubcluster{{Name: sc1}}},
		}
		vdb.Status.Subclusters = []v1.SubclusterStatus{
			{Name: sc1, Shutdown: true},
		}
		vdb.Status.Sandboxes = []v1.SandboxStatus{
			{Name: sc1, Subclusters: []string{sc1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts)).Should(Succeed())
		Expect(*sts.Spec.Replicas).Should(Equal(vdb.Spec.Subclusters[0].Size))

		vdb.Spec.Subclusters[0].Shutdown = true
		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &testPassword)
		pfacts.SandboxName = sc1
		r := MakeScaleInStatefulsetToZeroReconciler(vdbRec, vdb, &pfacts, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))

		newSts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), newSts)).Should(Succeed())
		Expect(*newSts.Spec.Replicas).Should(Equal(int32(0)))
	})

	It("should scale main cluster sts to zero if subcluster is shut down", func() {
		vdb := v1.MakeVDB()
		const sc1 = "sc1"
		const sc2 = "sc2"
		vdb.Spec.Subclusters = []v1.Subcluster{
			{Name: sc1, Size: 3, Shutdown: true},
			{Name: sc2, Size: 3, Shutdown: true},
		}

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		vdb.Status.Subclusters = []v1.SubclusterStatus{
			{Name: sc1, Shutdown: true},
			{Name: sc2, Shutdown: true},
		}

		sts1 := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), sts1)).Should(Succeed())
		Expect(*sts1.Spec.Replicas).Should(Equal(vdb.Spec.Subclusters[0].Size))
		sts2 := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), sts2)).Should(Succeed())
		Expect(*sts2.Spec.Replicas).Should(Equal(vdb.Spec.Subclusters[1].Size))

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, &testPassword)
		pfacts.SandboxName = v1.MainCluster
		r := MakeScaleInStatefulsetToZeroReconciler(vdbRec, vdb, &pfacts, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))

		newSts1 := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[0]), newSts1)).Should(Succeed())
		Expect(*newSts1.Spec.Replicas).Should(Equal(int32(0)))
		newSts2 := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, names.GenStsName(vdb, &vdb.Spec.Subclusters[1]), newSts2)).Should(Succeed())
		Expect(*newSts2.Spec.Replicas).Should(Equal(int32(0)))
		newVdb := &v1.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), newVdb)).Should(Succeed())
		Expect(newVdb.IsStatusConditionTrue(v1.MainClusterPodsTerminated)).Should(Equal(true))
	})
})
