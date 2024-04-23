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
	ctx := context.Background()

	It("Should return no error if sc is already down", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{3, 3}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		sc := &vdb.Spec.Subclusters[1]
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithUpNodeStateSet(ctx, vdb, sc, fpr, []int32{0, 1, 2}, false)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeStopSubclusterReconciler(vdbRec, logger, vdb, sc.Name, pfacts, dispatcher)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
	})

	It("Should fail if no subcluster is provided", func() {
		r := MakeStopSubclusterReconciler(vdbRec, logger, vapi.MakeVDB(), "", nil, nil)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).Should(ContainSubstring("no subcluster provided"))
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
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithUpNodeStateSet(ctx, vdb, sc, fpr, []int32{0, 1, 2}, true)
		setupAPIFunc := func(logr.Logger, string) (vadmin.VClusterProvider, logr.Logger) {
			return &mockvops.MockVClusterOps{}, logr.Logger{}
		}
		dispatcher := mockVClusterOpsDispatcherWithCustomSetup(vdb, setupAPIFunc)
		act := MakeStopSubclusterReconciler(vdbRec, logger, vdb, sc.Name, pfacts, dispatcher)
		r := act.(*StopSubclusterReconciler)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(r.InitiatorPodIP).Should(Equal("10.0.0.0"))
	})
})
