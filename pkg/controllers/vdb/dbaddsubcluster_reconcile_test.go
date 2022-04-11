/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("dbaddsubcluster_reconcile", func() {
	ctx := context.Background()

	It("should parse subcluster from vsql output", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		a := MakeDBAddSubclusterReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		r := a.(*DBAddSubclusterReconciler)
		subclusters := r.parseFetchSubclusterVsql(
			" sc1\n" +
				" sc2\n" +
				" sc3\n",
		)
		Expect(len(subclusters)).Should(Equal(3))
		for _, sc := range []string{"sc1", "sc2", "sc3"} {
			_, exists := subclusters[sc]
			Expect(exists).Should(BeTrue())
		}
	})

	It("should call AT to add a new subcluster", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Name = "sc1"
		// We only want a single pod created between both subclusters so that
		// the atPod is deterministic.
		vdb.Spec.Subclusters[0].Size = 1
		vdb.Spec.Subclusters = append(vdb.Spec.Subclusters, vapi.Subcluster{
			Name: "sc2",
			Size: 0,
		})
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		const PodIndex = 0
		atPod := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], PodIndex)
		// Ensure the fetch of subclusters does not list the second one (sc2)
		fpr.Results = cmds.CmdResults{
			atPod: []cmds.CmdResult{
				{Stdout: " sc1\n"},
			},
		}
		r := MakeDBAddSubclusterReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// Last command should be AT -t db_add_subcluster
		atCmdHistory := fpr.Histories[len(fpr.Histories)-1]
		Expect(atCmdHistory.Command).Should(ContainElements(
			"db_add_subcluster", "sc2",
		))
	})

	It("should use the proper subcluster type switch for v10.1.1 versions", func() {
		vdb := vapi.MakeVDB()
		vdb.ObjectMeta.Annotations[vapi.VersionAnnotation] = "v10.1.1-0"
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		act := MakeDBAddSubclusterReconciler(vdbRec, logger, vdb, fpr, &pfacts)
		r := act.(*DBAddSubclusterReconciler)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		r.ATPod = pfacts.Detail[names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)]

		vdb.Spec.Subclusters[0].IsPrimary = false
		Expect(r.createSubcluster(ctx, &vdb.Spec.Subclusters[0])).Should(Succeed())
		hists := fpr.FindCommands("db_add_subcluster")
		Expect(len(hists)).Should(Equal(1))
		Expect(hists[0].Command).Should(ContainElement("--is-secondary"))

		fpr.Histories = []cmds.CmdHistory{}
		vdb.Spec.Subclusters[0].IsPrimary = true
		Expect(r.createSubcluster(ctx, &vdb.Spec.Subclusters[0])).Should(Succeed())
		hists = fpr.FindCommands("db_add_subcluster")
		Expect(len(hists)).Should(Equal(1))
		Expect(hists[0].Command).ShouldNot(ContainElement("--is-primary"))
	})
})
