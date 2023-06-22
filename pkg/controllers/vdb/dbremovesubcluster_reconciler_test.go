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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("dbremovedsubcluster_reconcile", func() {
	ctx := context.Background()

	It("should do nothing if none of the statefulsets were created yet", func() {
		vdb := vapi.MakeVDB()
		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(vdbRec, fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		r := MakeDBRemoveSubclusterReconciler(vdbRec, logger, vdb, fpr, &pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should call AT to remove a new subcluster", func() {
		vdb := vapi.MakeVDB()
		scNames := []string{"sc1", "sc2"}
		scSizes := []int32{10, 5}
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: scNames[0], Size: scSizes[0]},
			{Name: scNames[1], Size: scSizes[1]},
		}
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateSvcs(ctx, k8sClient, vdb)
		defer test.DeleteSvcs(ctx, k8sClient, vdb)

		// We create a second vdb without one of the subclusters.  We then use
		// the finder to discover this additional subcluster.
		lookupVdb := vapi.MakeVDB()
		lookupVdb.Spec.Subclusters[0] = vapi.Subcluster{Name: scNames[0], Size: scSizes[0]}
		lookupVdb.Status.Subclusters = []vapi.SubclusterStatus{
			{Name: scNames[0], InstallCount: scSizes[0], AddedToDBCount: scSizes[0]},
			{Name: scNames[1], InstallCount: scSizes[1], AddedToDBCount: scSizes[1]},
		}

		fpr := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(fpr)
		dispatcher := vdbRec.makeDispatcher(logger, vdb, fpr)
		r := MakeDBRemoveSubclusterReconciler(vdbRec, logger, lookupVdb, fpr, pfacts, dispatcher)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// One command should be AT -t db_remove_subcluster and one should be
		// changing the default subcluster
		cmds := fpr.FindCommands("admintools -t db_remove_subcluster")
		Expect(len(cmds)).Should(Equal(1))
		cmds = fpr.FindCommands(fmt.Sprintf(`alter subcluster %q set default`, scNames[0]))
		Expect(len(cmds)).Should(Equal(1))
	})
})
