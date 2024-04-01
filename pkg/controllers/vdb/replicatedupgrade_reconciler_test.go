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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("replicatedupgrade_reconciler", func() {
	ctx := context.Background()

	It("should correctly assign replica groups for both subcluster types", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster, Size: 6},
			{Name: "sc2", Type: vapi.SecondarySubcluster, Size: 3},
			{Name: "sc3", Type: vapi.SecondarySubcluster, Size: 2},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		replicatedReconiler := createReplicatedUpgradeReconciler(vdb)
		Ω(replicatedReconiler.assignSubclustersToReplicaGroups(ctx)).Should(Equal(ctrl.Result{}))
		Ω(vdb.Status.UpgradeState).ShouldNot(BeNil())
		Ω(vdb.Status.UpgradeState.ReplicaGroups).Should(HaveLen(2))
		Ω(vdb.Status.UpgradeState.ReplicaGroups[0]).Should(ContainElements("sc1", "sc2", "sc3"))
	})
})

func createReplicatedUpgradeReconciler(vdb *vapi.VerticaDB) *ReplicatedUpgradeReconciler {
	fpr := &cmds.FakePodRunner{Results: cmds.CmdResults{}}
	pfacts := createPodFactsDefault(fpr)
	dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr, TestPassword)
	actor := MakeReplicatedUpgradeReconciler(vdbRec, logger, vdb, pfacts, dispatcher)
	return actor.(*ReplicatedUpgradeReconciler)
}
