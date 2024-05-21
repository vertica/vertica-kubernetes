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

	"github.com/go-logr/logr"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/mockvops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("stopsc_reconciler", func() {
	const sbName = "test-sb"
	ctx := context.Background()

	It("Should find subclusters that need to be shutdown", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{3, 3}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		const sbName2 = "test-sb2"
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName2, Subclusters: []vapi.SubclusterName{{Name: scNames[1]}}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{scNames[0]}},
			{Name: sbName2, Subclusters: []string{scNames[1]}},
		}

		pfacts := &PodFacts{NeedCollection: false, SandboxName: sbName}
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, vdbRec.EVRec, vadmin.SetupVClusterOps)
		act := MakeStopSubclusterReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
		r := act.(*StopSubclusterReconciler)
		scs := r.findSubclustersWithShutdownNeeded()
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0]).Should(Equal(scNames[0]))

		vdb.Status.Sandboxes = append(vdb.Status.Sandboxes, vapi.SandboxStatus{
			Name:        sbName,
			Subclusters: []string{scNames[0]},
		})
		vdb.Spec.Sandboxes = append(vdb.Spec.Sandboxes, vapi.Sandbox{
			Name:        sbName,
			Subclusters: []vapi.SubclusterName{{Name: scNames[0]}},
		})
		act = MakeStopSubclusterReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
		r = act.(*StopSubclusterReconciler)
		scs = r.findSubclustersWithShutdownNeeded()
		Expect(len(scs)).Should(Equal(0))
	})

	It("should requeue if pods are not running in sc to stop", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		sc := &vdb.Spec.Subclusters[0]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{sc.Name}},
		}
		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		pfacts.SandboxName = sbName
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, vdbRec.EVRec, vadmin.SetupVClusterOps)
		act := MakeStopSubclusterReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
		r := act.(*StopSubclusterReconciler)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
		Expect(err).Should(Succeed())
	})

	It("Should successfully stop a subcluster", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{3, 3}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		sc := &vdb.Spec.Subclusters[1]
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{sc.Name}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithUpNodeStateSet(ctx, vdb, sc, fpr, []int32{0, 1, 2}, true, sbName)
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &mockvops.MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
		act := MakeStopSubclusterReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
		r := act.(*StopSubclusterReconciler)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(r.PFacts.NeedCollection).Should(BeTrue())
	})
})
