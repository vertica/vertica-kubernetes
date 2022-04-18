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

var _ = Describe("k8s/version_reconcile", func() {
	ctx := context.Background()

	It("should update annotations in vdb since they differ", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{
				{
					Stdout: `Vertica Analytic Database v11.1.1-0
vertica(v11.1.0) built by @re-docker2 from tag@releases/VER_10_1_RELEASE_BUILD_10_20210413 on 'Wed Jun  2 2021' $BuildId$
`,
				},
			},
		}
		r := MakeVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, false)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(len(fetchVdb.ObjectMeta.Annotations)).Should(Equal(3))
		Expect(fetchVdb.ObjectMeta.Annotations[vapi.VersionAnnotation]).Should(Equal("v11.1.1-0"))
		Expect(fetchVdb.ObjectMeta.Annotations[vapi.BuildRefAnnotation]).Should(Equal("releases/VER_10_1_RELEASE_BUILD_10_20210413"))
		Expect(fetchVdb.ObjectMeta.Annotations[vapi.BuildDateAnnotation]).Should(Equal("Wed Jun  2 2021"))
	})

	It("should fail the reconciler if doing a downgrade", func() {
		vdb := vapi.MakeVDB()
		const OrigVersion = "v11.0.1"
		vdb.ObjectMeta.Annotations = map[string]string{
			vapi.VersionAnnotation: OrigVersion,
		}
		vdb.Spec.Subclusters[0].Size = 1
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pfacts := MakePodFacts(k8sClient, fpr)
		Expect(pfacts.Collect(ctx, vdb)).Should(Succeed())
		podName := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results = cmds.CmdResults{
			podName: []cmds.CmdResult{
				{
					Stdout: `Vertica Analytic Database v11.0.0-0
vertica(v11.1.0) built by @re-docker2 from tag@releases/VER_10_1_RELEASE_BUILD_10_20210413 on 'Wed Jun  2 2021' $BuildId$
`,
				},
			},
		}
		r := MakeVersionReconciler(vdbRec, logger, vdb, fpr, &pfacts, true)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))

		// Ensure we didn't update the vdb
		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.ObjectMeta.Annotations[vapi.VersionAnnotation]).Should(Equal(OrigVersion))
	})
})
