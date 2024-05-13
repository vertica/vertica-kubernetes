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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("sandboximage_reconciler", func() {
	ctx := context.Background()

	It("should set sandbox images in vdb", func() {
		vdb := vapi.MakeVDB()
		const (
			sb1 = "sb1"
			sb2 = "sb2"
			sb3 = "sb3"
			img = "vertica:test"
		)
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "default", Size: 1, Type: vapi.PrimarySubcluster},
			{Name: "sc1", Size: 1, Type: vapi.SecondarySubcluster},
			{Name: "sc2", Size: 1, Type: vapi.SecondarySubcluster},
			{Name: "sc3", Size: 1, Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{Name: sb1, Image: img, Subclusters: []vapi.SubclusterName{{Name: vdb.Spec.Subclusters[1].Name}}},
			{Name: sb2, Subclusters: []vapi.SubclusterName{{Name: vdb.Spec.Subclusters[2].Name}}},
			{Name: sb3, Subclusters: []vapi.SubclusterName{{Name: vdb.Spec.Subclusters[3].Name}}},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		fetchVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(vdb.Spec.Image).ShouldNot(Equal(""))
		Expect(fetchVdb.Spec.Sandboxes[0].Image).Should(Equal(img))
		Expect(fetchVdb.Spec.Sandboxes[1].Image).Should(Equal(""))
		Expect(fetchVdb.Spec.Sandboxes[2].Image).Should(Equal(""))

		r := MakeSandboxImageReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		// After reconcile, all sandboxes with empty image name should have the same
		// image set as spec.image
		Expect(k8sClient.Get(ctx, vapi.MakeVDBName(), fetchVdb)).Should(Succeed())
		Expect(fetchVdb.Spec.Sandboxes[0].Image).Should(Equal(img))
		Expect(fetchVdb.Spec.Sandboxes[1].Image).Should(Equal(vdb.Spec.Image))
		Expect(fetchVdb.Spec.Sandboxes[2].Image).Should(Equal(vdb.Spec.Image))
	})
})
