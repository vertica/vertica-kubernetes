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

package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
)

var _ = Describe("verticascrutinize_webhook", func() {
	const (
		validObjectName      = "test.test"
		validIncludePattern  = "testinc.*"
		validExcludePattern  = "testexc.*"
		validTargetNamespace = "target"
	)

	It("should succeed with default async options", func() {
		vrep := MakeVrep()
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should succeed with default sync options", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeSync
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should fail with invalid mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = "invalid"
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Mode must be either 'sync' or 'async'"))
	})

	It("should succeed if valid object name is used in async replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.ObjectName = validObjectName
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should succeed if valid include pattern is used in async replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.IncludePattern = validIncludePattern
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should succeed if valid exclude pattern is used in async replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.IncludePattern = validIncludePattern
		vrep.Spec.Source.ExcludePattern = validExcludePattern
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should succeed if valid target namespace is used in async replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Target.Namespace = validTargetNamespace
		_, err := vrep.ValidateCreate()
		Expect(err).Should(Succeed())
		_, err = vrep.ValidateUpdate(vrep)
		Expect(err).Should(Succeed())
	})

	It("should fail if object name is used in sync replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeSync
		vrep.Spec.Source.ObjectName = validObjectName
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Object name cannot be used in replication mode 'sync'"))
	})

	It("should fail if include pattern is used in sync replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeSync
		vrep.Spec.Source.IncludePattern = validIncludePattern
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Include pattern cannot be used in replication mode 'sync'"))
	})

	It("should fail if exclude pattern is used in sync replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeSync
		vrep.Spec.Source.IncludePattern = validIncludePattern
		vrep.Spec.Source.ExcludePattern = validExcludePattern
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Exclude pattern cannot be used in replication mode 'sync'"))
	})

	It("should fail if target namespace is used in sync replication mode", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeSync
		vrep.Spec.Target.Namespace = validTargetNamespace
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Target namespace cannot be used in replication mode 'sync'"))
	})

	It("should fail if object name and include pattern are used together", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.ObjectName = validObjectName
		vrep.Spec.Source.IncludePattern = validIncludePattern
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Object name and include pattern cannot be used together"))
	})

	It("should fail if object name and exclude pattern are used together", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.ObjectName = validObjectName
		vrep.Spec.Source.IncludePattern = validIncludePattern
		vrep.Spec.Source.ExcludePattern = validExcludePattern
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Object name and exclude pattern cannot be used together"))
	})

	It("should fail if exclude pattern is specified without include pattern", func() {
		vrep := MakeVrep()
		vrep.Spec.Mode = ReplicationModeAsync
		vrep.Spec.Source.ExcludePattern = validExcludePattern
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("Exclude pattern cannot be used without include pattern"))
	})

	It("should fail if poolingfrequency annotation is 0 or less than 0", func() {
		vrep := MakeVrep()
		vrep.Annotations = map[string]string{
			vmeta.ReplicationTimeoutAnnotation:          "10",
			vmeta.ReplicationPollingFrequencyAnnotation: "0",
		}
		_, err := vrep.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("polling frequency cannot be 0 or less than 0"))

	})

})
