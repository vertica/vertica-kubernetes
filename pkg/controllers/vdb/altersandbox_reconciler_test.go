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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("altersandbox_reconciler", func() {
	ctx := context.Background()

	It("should trigger configmap to alster sandbox subcluster type", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		const sandbox1 = "sand"
		const subcluster1 = "sc1"
		const subcluster2 = "sc2"
		const subcluster3 = "sc3"
		const tID = "12345"

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: subcluster1, Type: vapi.PrimarySubcluster, Size: 3},
			{Name: subcluster2, Type: vapi.SecondarySubcluster, Size: 3},
			{Name: subcluster3, Type: vapi.SecondarySubcluster, Size: 3},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sandbox1, Subclusters: []vapi.SandboxSubcluster{
				{Name: subcluster2, Type: vapi.PrimarySubcluster},
				// sc3 is the 2nd primary subcluster in sandbox
				// its type in db is secondary so it will be promoted to primary
				{Name: subcluster3, Type: vapi.PrimarySubcluster},
			}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sandbox1, Subclusters: []string{subcluster2, subcluster3}},
		}

		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pFacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		Expect(pFacts.Collect(ctx, vdb)).Should(Succeed())
		for _, sc := range vdb.Spec.Subclusters {
			// set sc pods to be up
			nm := names.GenPodName(vdb, &sc, 0)
			pFacts.Detail[nm] = &podfacts.PodFact{}
			pFacts.Detail[nm].SetUpNode(true)
			pFacts.Detail[nm].SetSubclusterName(sc.Name)
			if sc.Name == subcluster3 {
				pFacts.Detail[nm].SetIsPrimary(false) // set sc3 to secondary which is different to sandbox type
			} else {
				pFacts.Detail[nm].SetIsPrimary(true)
			}
			if sc.Name != subcluster1 {
				pFacts.Detail[nm].SetSandbox(sandbox1) // set sc2, sc3 to sandbox
			}
		}
		test.CreateConfigMap(ctx, k8sClient, vdb, vmeta.SandboxControllerAlterSubclusterTypeTriggerID, tID, sandbox1)
		defer test.DeleteConfigMap(ctx, k8sClient, vdb, sandbox1)

		// a new ID generated for alter sandbox type
		validateAlterSandboxReconcile(ctx, vdb, &pFacts)
		cm := &corev1.ConfigMap{}
		cmNm := names.GenSandboxConfigMapName(vdb, sandbox1)
		Expect(k8sClient.Get(ctx, cmNm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID]).ShouldNot(Equal(""))
		Expect(cm.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID]).ShouldNot(Equal(tID))

		// no new ID generated
		id := cm.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID]
		// reset sc3 to secondary which is the same to podfacts
		// should not trigger a configmap update
		vdb.Spec.Sandboxes[0].Subclusters[1].Type = vapi.SecondarySubcluster
		validateAlterSandboxReconcile(ctx, vdb, &pFacts)
		Expect(k8sClient.Get(ctx, cmNm, cm)).Should(Succeed())
		Expect(cm.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID]).Should(Equal(id))
	})
})

func validateAlterSandboxReconcile(ctx context.Context, vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts) {
	r := MakeAlterSandboxTypeReconciler(vdbRec, logger, vdb, pfacts)
	res, err := r.Reconcile(ctx, &ctrl.Request{})
	Expect(err).Should(Succeed())
	Expect(res).Should(Equal(ctrl.Result{Requeue: false}))
}
