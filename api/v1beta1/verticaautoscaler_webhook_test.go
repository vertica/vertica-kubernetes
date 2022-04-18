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

package v1beta1

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("verticaautoscaler_webhook", func() {
	It("should succeed with all valid fields", func() {
		vas := MakeVAS()
		Expect(vas.ValidateCreate()).Should(Succeed())
	})

	It("should fail if granularity isn't set properly", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = "BadValue"
		Expect(vas.ValidateCreate()).ShouldNot(Succeed())
	})

	It("should set a default value for the service name in the template", func() {
		vas := MakeVAS()
		vas.Spec.Template.ServiceName = ""
		vas.Default()
		Expect(vas.Spec.Template.ServiceName).Should(Equal(vas.Spec.ServiceName))
	})

	It("should fail if the service name differs", func() {
		vas := MakeVAS()
		vas.Spec.Template.ServiceName = "SomethingElse"
		Expect(vas.ValidateCreate()).ShouldNot(Succeed())
		vas.Spec.Template.ServiceName = ""
		Expect(vas.ValidateCreate()).Should(Succeed())
		Expect(vas.ValidateUpdate(MakeVAS())).ShouldNot(Succeed())
		vas.Spec.Template.ServiceName = vas.Spec.ServiceName
		Expect(vas.ValidateUpdate(MakeVAS())).Should(Succeed())
	})
})
