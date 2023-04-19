/*
 (c) Copyright [2021-2023] Open Text.
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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("stopdb_reconcile", func() {
	ctx := context.Background()

	It("should be a no-op if database doesn't exist", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsWithNoDB(ctx, vdb, fpr, int(vdb.Spec.Subclusters[0].Size))
		recon := MakeStopDBReconciler(vdbRec, vdb, fpr, pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(len(fpr.Histories)).Should(Equal(0))
	})

	It("should be a no-op if status condition isn't set", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		recon := MakeStopDBReconciler(vdbRec, vdb, fpr, &pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("stop_db")
		Expect(len(hist)).Should(Equal(0))
	})

	It("should attempt a stop_db if status condition is set", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		Expect(vdbstatus.UpdateCondition(ctx, k8sClient, vdb,
			vapi.VerticaDBCondition{Type: vapi.VerticaRestartNeeded, Status: corev1.ConditionTrue},
		)).Should(Succeed())
		Expect(vdb.IsConditionSet(vapi.VerticaRestartNeeded)).Should(BeTrue())

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		recon := MakeStopDBReconciler(vdbRec, vdb, fpr, pfacts)
		Expect(recon.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("stop_db")
		Expect(len(hist)).Should(Equal(1))

		// Status condition should be cleared
		Expect(vdb.IsConditionSet(vapi.VerticaRestartNeeded)).Should(BeFalse())
	})
})
