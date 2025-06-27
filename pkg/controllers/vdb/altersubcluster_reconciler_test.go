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

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{
				Name: "sc1",
				Type: vapi.PrimarySubcluster,
				Size: 3,
			},
			{
				// sc2 is the 2nd primary subcluster in main
				// its type in db is secondary so it will be promoted to primary
				Name: "sc2",
				Type: vapi.PrimarySubcluster,
				Size: 3,
			},
		}

		// pFacts.Collect requires UpNodeCount to be set
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: "sc1", Type: vapi.PrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
			{Name: "sc2", Type: vapi.PrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
		}

		// findSandboxSubclustersToAlter relys on podfacts status
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		fpr := &cmds.FakePodRunner{}
		pFacts := podfacts.PodFacts{VRec: vdbRec, PRunner: fpr, NeedCollection: true, Detail: make(podfacts.PodFactDetail)}
		Expect(pFacts.Collect(ctx, vdb)).Should(Succeed())

		// set sc2 pods to be up
		sc2 := &vdb.Spec.Subclusters[1]
		nm := names.GenPodName(vdb, sc2, 0)
		pFacts.Detail[nm].SetUpNode(true)
		pFacts.Detail[nm].SetIsPrimary(false)

		// find the subclusters to alter
		pFacts.SandboxName = sbName
		a := AlterSubclusterTypeReconciler{
			PFacts: &pFacts,
			Vdb:    vdb,
			Log:    logger,
		}
		scs, err := a.findMainSubclustersToAlter()
		Expect(err).Should(BeNil())
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0].Name).Should(Equal("sc2"))
	})

	It("should find sandbox subclusters to alter for upgrade", func() {
		vdb := vapi.MakeVDB()
		const sbName = "sand"

		vdb.Spec.Subclusters = []vapi.Subcluster{
			{
				Name: "sc1",
				Type: vapi.SecondarySubcluster,
				Size: 3,
			},
			{
				Name: "sc2",
				Type: vapi.PrimarySubcluster,
				Size: 3,
			},
			{
				// we don't support demote subcluster for now so
				// we are only going to find the subclusters that need
				// promotion
				Name: "sc3",
				Type: vapi.SecondarySubcluster,
				Size: 3,
			},
			{
				// sc4 is the 2nd primary subcluster in sandbox
				// its type in db is secondary so it will be promoted to primary
				Name: "sc4",
				Type: vapi.SecondarySubcluster,
				Size: 3,
			},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{
				Name: sbName,
				Subclusters: []vapi.SandboxSubcluster{
					{Name: "sc3", Type: vapi.PrimarySubcluster},
					{Name: "sc4", Type: vapi.PrimarySubcluster},
				},
			},
		}

		// pFacts.Collect requires UpNodeCount to be set
		vdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: "sc1", Type: vapi.SecondarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
			{Name: "sc2", Type: vapi.PrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
			{Name: "sc3", Type: vapi.SandboxPrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
			{Name: "sc4", Type: vapi.SandboxPrimarySubcluster, UpNodeCount: 3,
				Detail: []vapi.VerticaDBPodStatus{{Installed: true, AddedToDB: true}}},
		}

		// findSandboxSubclustersToAlter relys on podfacts status
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsNotRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		fpr := &cmds.FakePodRunner{}
		pFacts := podfacts.PodFacts{VRec: vdbRec, PRunner: fpr, NeedCollection: true, Detail: make(podfacts.PodFactDetail)}
		Expect(pFacts.Collect(ctx, vdb)).Should(Succeed())

		// set sc4 pods to be up
		sc := &vdb.Spec.Subclusters[3]
		nm := names.GenPodName(vdb, sc, 0)
		pFacts.Detail[nm].SetUpNode(true)
		pFacts.Detail[nm].SetIsPrimary(false)

		// find the subclusters to alter
		pFacts.SandboxName = sbName
		a := AlterSubclusterTypeReconciler{
			PFacts: &pFacts,
			Vdb:    vdb,
			Log:    logger,
		}
		scs, err := a.findSandboxSubclustersToAlter()
		Expect(err).Should(BeNil())
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0].Name).Should(Equal("sc4"))
	})
})
