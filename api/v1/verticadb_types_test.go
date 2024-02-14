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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("verticadb_types", func() {
	const FakeUID = "abcdef"

	It("should include UID in path if IncludeUIDInPath is set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Annotations[vmeta.IncludeUIDInPathAnnotation] = "true"
		Expect(vdb.GetCommunalPath()).Should(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should not include UID in path if IncludeUIDInPath is not set", func() {
		vdb := MakeVDB()
		vdb.ObjectMeta.UID = FakeUID
		vdb.Annotations[vmeta.IncludeUIDInPathAnnotation] = "false"
		Expect(vdb.GetCommunalPath()).ShouldNot(ContainSubstring(string(vdb.ObjectMeta.UID)))
	})

	It("should require a transient subcluster", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sc1"},
			{Name: "sc2"},
		}
		// Transient is only required if specified
		Expect(vdb.RequiresTransientSubcluster()).Should(BeFalse())
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Names: []string{"sc1"},
		}
		Expect(vdb.RequiresTransientSubcluster()).Should(BeFalse())
		vdb.Spec.TemporarySubclusterRouting = &SubclusterSelection{
			Template: Subcluster{
				Name: "the-transient-sc-name",
				Size: 1,
				Type: TransientSubcluster,
			},
		}
		Expect(vdb.RequiresTransientSubcluster()).Should(BeTrue())
	})

	It("should return the first primary subcluster", func() {
		vdb := MakeVDB()
		vdb.Spec.Subclusters = []Subcluster{
			{Name: "sec1", Type: SecondarySubcluster, Size: 1},
			{Name: "sec2", Type: SecondarySubcluster, Size: 1},
			{Name: "pri1", Type: PrimarySubcluster, Size: 1},
			{Name: "pri2", Type: PrimarySubcluster, Size: 1},
		}
		sc := vdb.GetFirstPrimarySubcluster()
		Ω(sc).ShouldNot(BeNil())
		Ω(sc.Name).Should(Equal("pri1"))
	})

	It("should generate httpstls.json for some versions", func() {
		vdb := MakeVDB()
		// Annotation takes precedence
		vdb.Annotations[vmeta.HTTPSTLSConfGenerationAnnotation] = vmeta.HTTPSTLSConfGenerationAnnotationFalse
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeFalse())
		vdb.Annotations[vmeta.HTTPSTLSConfGenerationAnnotation] = vmeta.HTTPSTLSConfGenerationAnnotationTrue
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeTrue())
		delete(vdb.Annotations, vmeta.HTTPSTLSConfGenerationAnnotation)
		// Fail if no version is set
		delete(vdb.Annotations, vmeta.VersionAnnotation)
		_, err := vdb.IsHTTPSTLSConfGenerationEnabled()
		Ω(err).ShouldNot(Succeed())
		vdb.Annotations[vmeta.VersionAnnotation] = "v11.0.0"
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeTrue())
		vdb.Annotations[vmeta.VersionAnnotation] = "v24.1.0"
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeFalse())
		// Applies only for create, not revive
		vdb.Spec.InitPolicy = CommunalInitPolicyRevive
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeTrue())
		vdb.Spec.InitPolicy = CommunalInitPolicyCreateSkipPackageInstall
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeFalse())
		// If database is already created, then we assume we have to generate it.
		cond := MakeCondition(DBInitialized, metav1.ConditionTrue, "Initialized")
		meta.SetStatusCondition(&vdb.Status.Conditions, *cond)
		Ω(vdb.IsHTTPSTLSConfGenerationEnabled()).Should(BeTrue())
	})
})
