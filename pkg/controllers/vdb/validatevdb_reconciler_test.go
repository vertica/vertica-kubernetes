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
)

var _ = Describe("ValidateVDBReconciler", func() {
	var reconciler *ValidateVDBReconciler
	var vdb *vapi.VerticaDB

	ctx := context.Background()

	BeforeEach(func() {
		vdb = vapi.MakeVDBForVclusterOps()
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{Name: "sc1", Type: vapi.PrimarySubcluster},
			{Name: "sc2", Type: vapi.SandboxPrimarySubcluster},
			{Name: "sc3", Type: vapi.SecondarySubcluster},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{
				Name: "sb1",
				Subclusters: []vapi.SandboxSubcluster{
					{Name: "sc2", Type: vapi.PrimarySubcluster},
					{Name: "sc3", Type: vapi.PrimarySubcluster},
				},
			},
		}
		test.CreateVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)

		rec := MakeValidateVDBReconciler(vdbRec, logger, vdb)
		reconciler = rec.(*ValidateVDBReconciler)
	})

	AfterEach(func() {
		test.DeleteVDB(ctx, k8sClient, vdb)
		test.DeletePods(ctx, k8sClient, vdb)
	})

	It("should update subcluster types from sandboxprimary to secondary", func() {
		scsMain, scsSandbox, err := reconciler.validateSubclusters()
		Expect(err).ShouldNot(HaveOccurred())
		_, err = reconciler.updateSubclusters(ctx, scsMain, scsSandbox)
		Expect(err).To(Succeed())
		Expect(vdb.Spec.Subclusters[1].Type).To(Equal(vapi.SecondarySubcluster))
	})

	It("should update sandbox subcluster types from primary to secondary", func() {
		scsMain, scsSandbox, err := reconciler.validateSubclusters()
		Expect(err).ShouldNot(HaveOccurred())
		_, err = reconciler.updateSubclusters(ctx, scsMain, scsSandbox)
		Expect(err).To(Succeed())
		Expect(vdb.Spec.Sandboxes[0].Subclusters[1].Type).To(Equal(vapi.SecondarySubcluster))
	})

	It("should not update subcluster types if already valid", func() {
		// set valid subcluster types
		vdb.Spec.Subclusters[1].Type = vapi.SecondarySubcluster
		scsMain, scsSandbox, err := reconciler.validateSubclusters()
		Expect(err).ShouldNot(HaveOccurred())
		_, err = reconciler.updateSubclusters(ctx, scsMain, scsSandbox)
		Expect(err).To(Succeed())
		Expect(vdb.Spec.Sandboxes[0].Subclusters[1].Type).To(Equal(vapi.PrimarySubcluster))
	})
})
