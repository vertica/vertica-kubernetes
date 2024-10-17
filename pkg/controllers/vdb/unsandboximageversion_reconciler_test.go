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
	appsv1 "k8s.io/api/apps/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verticaimage_reconciler", func() {
	ctx := context.Background()
	mainClusterImage := "vertica-k8s:20240404"
	sandboxImage := "vertica-k8s:20250505"

	It("should recreate the statefulset when a secondary subcluster has a different image", func() {
		vdb := vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: maincluster, Size: 1, Type: vapi.PrimarySubcluster},
			{Name: subcluster1, Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Image = mainClusterImage
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		Expect(k8sClient.Status().Update(ctx, vdb)).Should(Succeed())

		fpr := &cmds.FakePodRunner{}
		pfacts := podfacts.MakePodFacts(vdbRec, fpr, logger, TestPassword)
		rec := MakeUnsandboxImageVersionReconciler(vdbRec, vdb, logger, &pfacts)
		r := rec.(*UnsandboxImageVersion)
		Expect(r.PFacts.Collect(ctx, vdb)).Should(Succeed())
		pn := names.GenPodName(vdb, &vdb.Spec.Subclusters[1], 0)
		sc1Pf := r.PFacts.Detail[pn]
		// let subcluster1 have a different image, its statefulset
		// should be recreated
		sc1Pf.SetImage(sandboxImage)
		Expect(r.reconcileVerticaImage(ctx)).Should(Equal(ctrl.Result{}))

		sc1 := &vdb.Spec.Subclusters[0]
		sc1StsName := names.GenStsName(vdb, sc1)
		sts := &appsv1.StatefulSet{}
		Expect(k8sClient.Get(ctx, sc1StsName, sts)).Should(Succeed())
		// the new statefulset should have the same image as primary subclusters in main cluster
		Expect(sts.Spec.Template.Spec.Containers[0].Image).Should(Equal(mainClusterImage))
	})
})
