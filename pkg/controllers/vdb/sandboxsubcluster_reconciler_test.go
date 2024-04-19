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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// initPFacts is a helper function to initialize pod facts with some test information
func initPFacts(pfacts *PodFacts, vdb *vapi.VerticaDB, sc1, sc2 string) (pfmain, pfsc1, pfsc2 types.NamespacedName) {
	pfmain = names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
	pfacts.Detail[pfmain] = &PodFact{}
	pfacts.Detail[pfmain].upNode = true
	pfacts.Detail[pfmain].subclusterName = ""
	pfacts.Detail[pfmain].isPrimary = true
	pfsc1 = names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
	pfacts.Detail[pfsc1] = &PodFact{}
	pfacts.Detail[pfsc1].upNode = true
	pfacts.Detail[pfsc1].subclusterName = sc1
	pfsc2 = names.GenPodName(vdb, &vdb.Spec.Subclusters[2], 0)
	pfacts.Detail[pfsc2] = &PodFact{}
	pfacts.Detail[pfsc2].upNode = true
	pfacts.Detail[pfsc2].subclusterName = sc2
	return pfmain, pfsc1, pfsc2
}

var _ = Describe("sandboxsubcluster_reconcile", func() {
	ctx := context.Background()
	maincluster := "main"
	subcluster1 := "sc1"
	subcluster2 := "sc2"
	sandbox1 := "sandbox1"
	sandbox2 := "sandbox2"

	It("should exit without error if no sandboxes specified", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit without error if not using an EON database", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.ShardCount = 0 // Force enterprise database
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		Expect(vdb.IsEON()).Should(BeFalse())
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit without error if using schedule-only policy", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit without error if subclusters are already sandboxed", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		_, pfsc1, pfsc2 := initPFacts(&pfacts, vdb, subcluster1, subcluster2)
		// subcluster1 and subcluster2 are sandboxed
		pfacts.Detail[pfsc1].sandbox = sandbox1
		pfacts.Detail[pfsc2].sandbox = sandbox2
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit with error if the nodes in main cluster or subclusters are not UP", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		pfmain, pfsc1, _ := initPFacts(&pfacts, vdb, subcluster1, subcluster2)
		// let subcluster1 down
		pfacts.Detail[pfsc1].upNode = false
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).Should(ContainSubstring("the pod does not contain an UP Vertica node"))

		// let subcluster1 up and main cluster down
		pfacts.Detail[pfsc1].upNode = true
		pfacts.Detail[pfmain].upNode = false
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err.Error()).Should(ContainSubstring("cannot find an UP node in main cluster"))
	})

	It("should sandbox the correct subclusters", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: "sc3", Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		_, _, _ = initPFacts(&pfacts, vdb, subcluster1, subcluster2)
		pfsc3 := names.GenPodName(vdb, &vdb.Spec.Subclusters[3], 0)
		pfacts.Detail[pfsc3] = &PodFact{}
		pfacts.Detail[pfsc3].upNode = true
		pfacts.Detail[pfsc3].subclusterName = "sc3"
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		rec := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		r := rec.(*SandboxSubclusterReconciler)
		scSbMap, err := r.fetchSubclustersWithSandboxes()
		targetScSbMap := map[string]string{subcluster1: sandbox1, subcluster2: sandbox2}
		Expect(scSbMap).Should(Equal(targetScSbMap))
		Expect(err).Should(BeNil())
	})

	It("should update the sandbox status correctly", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
			{Name: subcluster2, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
			{Name: sandbox2, Subclusters: []vapi.SubclusterName{{Name: subcluster2}}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr, logger, TestPassword)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		rec := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		r := rec.(*SandboxSubclusterReconciler)
		sbScMap := map[string][]string{sandbox1: {subcluster1}, sandbox2: {subcluster2}}
		err := r.updateSandboxStatus(ctx, sbScMap)
		Expect(err).Should(BeNil())

		// status should be updated
		targetSandboxStatus := []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
			{Name: sandbox2, Subclusters: []string{subcluster2}},
		}
		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		Expect(newVdb.Status.Sandboxes).Should(Equal(targetSandboxStatus))
	})
})
