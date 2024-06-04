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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
)

var _ = Describe("altersubcluster_reconcile", func() {

	It("should find subclusters to alter", func() {
		vdb := vapi.MakeVDB()
		annPri := map[string]string{
			vmeta.ParentSubclusterTypeAnnotation: vapi.PrimarySubcluster,
		}
		annSec := map[string]string{
			vmeta.ParentSubclusterTypeAnnotation: vapi.SecondarySubcluster,
		}
		const sbName = "sand"
		vdb.Spec.Subclusters = []vapi.Subcluster{
			{
				Name: "sc1",
				Type: vapi.SecondarySubcluster,
			},
			{
				Name: "sc2",
				Type: vapi.PrimarySubcluster,
			},
			{
				Name:        "sc3",
				Type:        vapi.SandboxPrimarySubcluster,
				Annotations: annSec,
			},
			{
				Name:        "sc4",
				Type:        vapi.SecondarySubcluster,
				Annotations: annPri,
			},
		}
		vdb.Spec.Sandboxes = []vapi.Sandbox{
			{
				Name: sbName,
				Subclusters: []vapi.SubclusterName{
					{Name: "sc3"},
					{Name: "sc4"},
				},
			},
		}
		a := AlterSubclusterTypeReconciler{
			PFacts: &PodFacts{SandboxName: sbName},
			Vdb:    vdb,
			Log:    logger,
		}
		scs, err := a.findSandboxSubclustersToAlter()
		Expect(err).Should(BeNil())
		Expect(len(scs)).Should(Equal(1))
		Expect(scs[0].Name).Should(Equal("sc4"))
	})
})
