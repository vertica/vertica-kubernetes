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

package sandbox

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	vdbcontroller "github.com/vertica/vertica-kubernetes/pkg/controllers/vdb"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("sandboxsubcluster_reconcile", func() {
	ctx := context.Background()
	maincluster := "main"
	subcluster1 := "sc1"
	sandbox1 := "sandbox1"

	It("should exit without error if not using an EON database", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.ShardCount = 0 // Force enterprise database
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		r := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, nil)
		Expect(vdb.IsEON()).Should(BeFalse())
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should exit without error if using schedule-only policy", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.InitPolicy = vapi.CommunalInitPolicyScheduleOnly
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		r := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, nil)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should update expired sandbox status", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		rec := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, nil)
		r := rec.(*UnsandboxSubclusterReconciler)
		Expect(r.PFacts.Collect(ctx, vdb)).Should(Succeed())
		// fill the sandbox status with wrong info, we expect the wrong info to be cleaned
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())
		Expect(r.reconcileSandboxStatus(ctx)).Should(BeNil())

		// sandbox status should be updated
		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		Expect(newVdb.Status.Sandboxes).Should(BeEmpty())
	})

	It("should delete expired sandbox config map", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		nm := names.GenSandboxConfigMapName(vdb, sandbox1)
		cm := builder.BuildSandboxConfigMap(nm, vdb, sandbox1)
		Expect(k8sClient.Create(ctx, cm)).Should(Succeed())
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		rec := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, cm)
		r := rec.(*UnsandboxSubclusterReconciler)
		// now sandbox status is empty, the config map should be deleted
		err, deleted := r.reconcileSandboxConfigMap(ctx)
		Expect(err).Should(BeNil())
		Expect(deleted).Should(BeTrue())
		// the map should be deleted and cannot be found any more
		oldCM := &corev1.ConfigMap{}
		err = r.Client.Get(ctx, nm, oldCM)
		Expect(kerrors.IsNotFound(err)).Should(BeTrue())
	})

	It("should update the sandbox status correctly", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster1}},
		}
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		rec := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, nil)
		r := rec.(*UnsandboxSubclusterReconciler)
		// after we removed subcluster1 from sandbox1, we will update sandbox status
		Expect(r.updateSandboxStatus(ctx, sandbox1, []string{subcluster1})).Should(BeNil())

		// sandbox status should be empty
		newVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), newVdb)).Should(Succeed())
		Expect(newVdb.Status.Sandboxes).Should(BeEmpty())
	})

	It("should delete a sandbox config map correctly", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		nm := names.GenSandboxConfigMapName(vdb, sandbox1)
		cm := builder.BuildSandboxConfigMap(nm, vdb, sandbox1)
		Expect(k8sClient.Create(ctx, cm)).Should(Succeed())
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := vdbcontroller.MakePodFacts(sbRec, fpr, logger, TestPassword)
		dispatcher := vadmin.MakeVClusterOps(logger, vdb, k8sClient, TestPassword, sbRec.EVRec, vadmin.SetupVClusterOps)
		rec := MakeUnsandboxSubclusterReconciler(sbRec, vdb, logger, k8sClient, &pfacts, dispatcher, cm)
		r := rec.(*UnsandboxSubclusterReconciler)
		// after we removed subcluster1 from sandbox1, we will delete the config map
		Expect(r.deleteConfigMap(ctx)).Should(BeNil())
		// the map should be deleted and cannot be found any more
		oldCM := &corev1.ConfigMap{}
		err := r.Client.Get(ctx, nm, oldCM)
		Expect(kerrors.IsNotFound(err)).Should(BeTrue())
	})
})
