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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("status_reconcile", func() {
	ctx := context.Background()

	It("should update the installed count when all pods are installed", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount).Should(Equal(int32(3)))

		// vdb should be updated too
		Expect(vdb.Status.Subclusters[0].InstallCount).Should(Equal(int32(3)))
	})

	It("should not fail if no objects exist yet in the crd", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		// We intentionally don't create the pods or sts

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		stat := &fetchVdb.Status
		Expect(stat.InstallCount).Should(Equal(int32(0)))
		Expect(stat.SubclusterCount).Should(Equal(int32(1)))
	})

	It("should handle multiple subclusters", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{Name: "other", Size: 4})
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].AddedToDBCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].UpNodeCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[1].InstallCount).Should(Equal(int32(4)))
		Expect(fetchVdb.Status.Subclusters[1].AddedToDBCount).Should(Equal(int32(4)))
		Expect(fetchVdb.Status.Subclusters[1].UpNodeCount).Should(Equal(int32(4)))
	})

	It("should only count installed and db existence for pods that are running", func() {
		vdb := vapi.MakeVDB()
		const ScIndex = 0
		sc := vdb.Spec.Subclusters[ScIndex]
		sc.Size = 2
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		// Make only 1 pod running
		const PodIndex = 1
		test.SetPodStatus(ctx, k8sClient, 1 /* funcOffset */, names.GenPodName(vdb, &sc, PodIndex), ScIndex, PodIndex, test.AllPodsRunning)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[ScIndex].InstallCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[ScIndex].AddedToDBCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[ScIndex].UpNodeCount).Should(Equal(int32(1)))
	})

	It("should remove old status when we scale down", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 5
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount).Should(Equal(int32(5)))

		test.ScaleDownSubcluster(ctx, k8sClient, vdb, sc, 2)
		pfacts.Invalidate()

		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount).Should(Equal(int32(2)))
	})
})
