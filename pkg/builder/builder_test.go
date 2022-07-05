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

package builder

import (
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	v1 "k8s.io/api/core/v1"
)

var _ = Describe("builder", func() {
	It("should generate identical k8s containers each time", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Annotations = map[string]string{
			"key1":                     "val1",
			"key2":                     "val2",
			"vertica.com/gitRef":       "abcd123",
			"1_not_valid_env_var_name": "blah",
		}

		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		const MaxLoopIteratons = 100
		for i := 1; i < MaxLoopIteratons; i++ {
			c := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
			Expect(reflect.DeepEqual(c, baseContainer)).Should(BeTrue())
		}
	})

	It("should add our own capabilities to the securityContext", func() {
		vdb := vapi.MakeVDB()
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT", "AUDIT_WRITE", "SYS_PTRACE"}))
	})

	It("should add omit our own capabilities in the securityContext if we are dropping them", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Capabilities: &v1.Capabilities{
				Drop: []v1.Capability{"AUDIT_WRITE"},
			},
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Capabilities.Add).Should(ContainElements([]v1.Capability{"SYS_CHROOT", "SYS_PTRACE"}))
		Expect(baseContainer.SecurityContext.Capabilities.Add).ShouldNot(ContainElement([]v1.Capability{"AUDIT_WRITE"}))
	})

	It("should allow you to run in priv mode", func() {
		vdb := vapi.MakeVDB()
		priv := true
		vdb.Spec.SecurityContext = &v1.SecurityContext{
			Privileged: &priv,
		}
		baseContainer := makeServerContainer(vdb, &vdb.Spec.Subclusters[0])
		Expect(baseContainer.SecurityContext).ShouldNot(BeNil())
		Expect(baseContainer.SecurityContext.Privileged).ShouldNot(BeNil())
		Expect(*baseContainer.SecurityContext.Privileged).Should(BeTrue())
	})
})
