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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
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

		pfacts := createPodFactsDefault(&cmds.FakePodRunner{})
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(3)))

		// vdb should be updated too
		Expect(vdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(3)))
	})

	It("should not fail if no objects exist yet in the crd", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		// We intentionally don't create the pods or sts

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		stat := &fetchVdb.Status
		Expect(stat.InstallCount()).Should(Equal(int32(0)))
		Expect(stat.SubclusterCount).Should(Equal(int32(1)))
	})

	It("Should not remove sandboxed subclusters status", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{1, 2}
		const sbName = "sand"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: scNames[1:]},
		}
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: scNames[1], AddedToDBCount: scSizes[1], Detail: []vapi.VerticaDBPodStatus{
				{Installed: true, AddedToDB: true},
				{Installed: true, AddedToDB: true},
			}, UpNodeCount: scSizes[1]},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.Status.Sandboxes)).Should(Equal(1))
		Expect(len(fetchVdb.Status.Subclusters)).Should(Equal(2))
		Expect(fetchVdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].AddedToDBCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].UpNodeCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[1].InstallCount()).Should(Equal(int32(2)))
		Expect(fetchVdb.Status.Subclusters[1].AddedToDBCount).Should(Equal(int32(2)))
		Expect(fetchVdb.Status.Subclusters[1].UpNodeCount).Should(Equal(int32(2)))

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
		pfacts := createPodFactsDefault(fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].AddedToDBCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[0].UpNodeCount).Should(Equal(int32(1)))
		Expect(fetchVdb.Status.Subclusters[1].InstallCount()).Should(Equal(int32(4)))
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
		pfacts := createPodFactsDefault(fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[ScIndex].InstallCount()).Should(Equal(int32(1)))
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

		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: sc.Name, AddedToDBCount: sc.Size, Detail: []vapi.VerticaDBPodStatus{
				{Installed: true, AddedToDB: true},
				{Installed: true, AddedToDB: true},
				{Installed: true, AddedToDB: true},
				{Installed: true, AddedToDB: true},
				{Installed: true, AddedToDB: true},
			}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(5)))

		test.ScaleDownSubcluster(ctx, k8sClient, vdb, sc, 2)
		pfacts.Invalidate()

		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].InstallCount()).Should(Equal(int32(2)))
	})

	It("should preserve old status when subclusters is not running", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		sc.Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		const Oid = "123456"
		const VNode = "v_vertdb_node0001"
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{
				Name:   sc.Name,
				Oid:    Oid,
				Detail: []vapi.VerticaDBPodStatus{{VNodeName: VNode}},
			},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfacts.Detail = nil // Mock no details for pods
		r := MakeStatusReconciler(k8sClient, scheme.Scheme, logger, vdb, pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Status.Subclusters[0].Oid).Should(Equal(Oid))
		Expect(fetchVdb.Status.Subclusters[0].Detail[0].VNodeName).Should(Equal(VNode))
	})
})
