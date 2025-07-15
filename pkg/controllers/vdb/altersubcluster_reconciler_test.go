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
)

var _ = Describe("altersubcluster_reconcile", func() {
	ctx := context.Background()

	It("should find main subclusters to alter for upgrade", func() {
		vdb := vapi.MakeVDB()
		const sbName = "sand"
		const subcluster1 = "sc1"
		const subcluster2 = "sc2"

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: subcluster1, Type: vapi.PrimarySubcluster, Size: 3},
			// sc2 is the 2nd primary subcluster in main
			// its type in db is secondary so it will be promoted to primary
			{Name: subcluster2, Type: vapi.PrimarySubcluster, Size: 3},
		}

		// pFacts.Collect requires UpNodeCount to be set
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: subcluster1, Type: vapi.PrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
			{Name: subcluster2, Type: vapi.PrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
		}

		// findSandboxSubclustersToAlter relys on podfacts status
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		fpr := &cmds.FakePodRunner{}
		pFacts := podfacts.PodFacts{VRec: vdbRec, PRunner: fpr, NeedCollection: true, Detail: make(podfacts.PodFactDetail)}
		Expect(pFacts.Collect(ctx, vdb)).Should(Succeed())

		// set sc1 pods to be up
		sc1 := &vdb.Spec.Subclusters[0]
		nmSc1 := names.GenPodName(vdb, sc1, 0)
		pFacts.Detail[nmSc1].SetUpNode(true)
		pFacts.Detail[nmSc1].SetIsPrimary(true)

		// set sc2 pods to be up
		sc2 := &vdb.Spec.Subclusters[1]
		nmSc2 := names.GenPodName(vdb, sc2, 0)
		pFacts.Detail[nmSc2].SetUpNode(true)
		pFacts.Detail[nmSc2].SetIsPrimary(false)

		// find the subclusters to alter
		pFacts.SandboxName = sbName
		a := AlterSubclusterTypeReconciler{
			PFacts: &pFacts,
			Vdb:    vdb,
			Log:    logger,
		}
		_, scs, err := a.findSubclustersToAlter()
		Expect(err).Should(BeNil())
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0]).Should(Equal(subcluster2))
	})

	It("should find sandbox subclusters to alter for upgrade", func() {
		vdb := vapi.MakeVDB()
		const sbName = "sand"
		const subcluster1 = "sc1"
		const subcluster2 = "sc2"
		const subcluster3 = "sc3"
		const subcluster4 = "sc4"

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: subcluster1, Type: vapi.PrimarySubcluster, Size: 3},
			{Name: subcluster2, Type: vapi.SecondarySubcluster, Size: 3},
			{Name: subcluster3, Type: vapi.SecondarySubcluster, Size: 3},
			{Name: subcluster4, Type: vapi.SecondarySubcluster, Size: 3},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sbName, Subclusters: []vapi.SandboxSubcluster{
				{Name: subcluster2, Type: vapi.PrimarySubcluster},
				// sc3 is the 2nd primary subcluster in sandbox
				// its type in db is secondary so it will be promoted to primary
				{Name: subcluster3, Type: vapi.PrimarySubcluster},
				// sc4 is the secondary subcluster in sandbox
				// but its type in db is primary so it will be demoted to secondary
				{Name: subcluster4, Type: vapi.SecondarySubcluster},
			}},
		}
		vdb.Status.Sandboxes = []vapi.SandboxStatus{
			{Name: sbName, Subclusters: []string{subcluster2, subcluster3, subcluster4}},
		}

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
				pFacts.Detail[nm].SetIsPrimary(true) // set sc4 to primary which is different to sandbox type
			}
			if sc.Name != subcluster1 {
				pFacts.Detail[nm].SetSandbox(sbName) // set sc2, sc3, sc4 to sandbox
			}
		}

		// find the subclusters to alter
		pFacts.SandboxName = sbName
		a := AlterSubclusterTypeReconciler{
			PFacts: &pFacts,
			Vdb:    vdb,
			Log:    logger,
		}
		_, scs, err := a.findSubclustersToAlter()
		Expect(err).Should(BeNil())
		Expect(len(scs)).Should(Equal(2))
		Expect(scs[0]).Should(Equal(subcluster3))
		Expect(scs[1]).Should(Equal(subcluster4))
	})
})
