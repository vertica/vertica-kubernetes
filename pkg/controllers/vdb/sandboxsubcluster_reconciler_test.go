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
func initPFacts(pfacts *PodFacts, vdb *vapi.VerticaDB, sc1, sc2 string) (pfmain, pfsc1 types.NamespacedName) {
	pfmain = names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
	pfacts.Detail[pfmain] = &PodFact{}
	pfacts.Detail[pfmain].upNode = true
	pfacts.Detail[pfmain].subclusterName = ""
	pfacts.Detail[pfmain].isPrimary = true
	pfsc1 = names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
	pfacts.Detail[pfsc1] = &PodFact{}
	pfacts.Detail[pfsc1].upNode = true
	pfacts.Detail[pfsc1].subclusterName = sc1
	pfsc2 := names.GenPodName(vdb, &vdb.Spec.Subclusters[2], 0)
	pfacts.Detail[pfsc2] = &PodFact{}
	pfacts.Detail[pfsc2].upNode = true
	pfacts.Detail[pfsc2].subclusterName = sc2
	return pfmain, pfsc1
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
		pfacts := PodFacts{}
		pfacts.Detail = make(PodFactDetail)
		pfmain := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		pfacts.Detail[pfmain] = &PodFact{}
		pfacts.Detail[pfmain].upNode = true
		pfacts.Detail[pfmain].subclusterName = ""
		pfacts.Detail[pfmain].isPrimary = true
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should requeue if the nodes in main cluster or subclusters are not UP", func() {
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
		pfmain, pfsc1 := initPFacts(&pfacts, vdb, subcluster1, subcluster2)
		// let subcluster1 down
		// should requeue the iteration without any error
		pfacts.Detail[pfsc1].upNode = false
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		r := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		// let subcluster1 up and main cluster down
		// should requeue the iteration without any error
		pfacts.Detail[pfsc1].upNode = true
		pfacts.Detail[pfmain].upNode = false
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
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
		_, _ = initPFacts(&pfacts, vdb, subcluster1, subcluster2)
		pfsc3 := names.GenPodName(vdb, &vdb.Spec.Subclusters[3], 0)
		pfacts.Detail[pfsc3] = &PodFact{}
		pfacts.Detail[pfsc3].upNode = true
		pfacts.Detail[pfsc3].subclusterName = "sc3"
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
		rec := MakeSandboxSubclusterReconciler(vdbRec, logger, vdb, &pfacts, dispatcher, k8sClient)
		r := rec.(*SandboxSubclusterReconciler)
		scSbMap, allNodesUp := r.fetchSubclustersWithSandboxes()
		targetScSbMap := map[string]string{subcluster1: sandbox1, subcluster2: sandbox2}
		Expect(scSbMap).Should(Equal(targetScSbMap))
		Expect(allNodesUp).Should(BeTrue())
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
		Expect(newVdb.Status.Sandboxes).Should(ConsistOf(targetSandboxStatus))
	})

	It("should create and update a sandbox config map correctly", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 3, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SubclusterName{{Name: subcluster1}}},
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
		// should create config map for sandbox1
		err := r.checkSandboxConfigMap(ctx, sandbox1)
		Expect(err).Should(BeNil())
		nm := names.GenSandboxConfigMapName(r.Vdb, sandbox1)
		defer deleteConfigMap(ctx, r.Vdb, nm.Name)

		// verify the content of the config map
		cm, res, err := getConfigMap(ctx, r.VRec, r.Vdb, nm)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(cm.Data[vapi.SandboxNameKey]).Should(Equal(sandbox1))
		Expect(cm.Data[vapi.VerticaDBNameKey]).Should(Equal(r.Vdb.Spec.DBName))

		testAnnotation := "test-annotation"
		testValue := "test-value"
		r.Vdb.Spec.Annotations = make(map[string]string)
		r.Vdb.Spec.Annotations[testAnnotation] = testValue
		// should update config map for sandbox1
		err = r.checkSandboxConfigMap(ctx, sandbox1)
		Expect(err).Should(BeNil())

		// verify the content of the config map
		cm, res, err = getConfigMap(ctx, r.VRec, r.Vdb, nm)
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(cm.Data[vapi.SandboxNameKey]).Should(Equal(sandbox1))
		Expect(cm.Data[vapi.VerticaDBNameKey]).Should(Equal(r.Vdb.Spec.DBName))
		Expect(cm.Annotations[testAnnotation]).Should(Equal(testValue))
	})
})
