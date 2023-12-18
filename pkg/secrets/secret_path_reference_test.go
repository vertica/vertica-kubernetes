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

package secrets

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("secrets/secret_path_reference", func() {
	It("should parse secret name for GSM", func() {
		Ω(IsGSMSecret("gsm://projects/blah/mysecret/versions/1")).Should(BeTrue())
		Ω(RemovePathReference("gsm://projects/blah/mysecret/versions/1")).Should(Equal("projects/blah/mysecret/versions/1"))
	})

	It("should parse secret name for AWS Secrets Manager", func() {
		Ω(IsAWSSecretsManagerSecret("awssm://aws-secrets-manager-secret")).Should(BeTrue())
		Ω(RemovePathReference("awssm://aws-secrets-manager-secret")).Should(Equal("aws-secrets-manager-secret"))
	})

	It("should parse secret name for k8s as a fallback", func() {
		Ω(IsK8sSecret("secret")).Should(BeTrue())
		Ω(RemovePathReference("secret")).Should(Equal("secret"))
		sn := "abc://default-to-k8s-if-unknown-path-reference"
		Ω(IsK8sSecret(sn)).Should(BeTrue())
		Ω(RemovePathReference(sn)).Should(Equal("abc://default-to-k8s-if-unknown-path-reference"))
		Ω(IsGSMSecret(sn)).Should(BeFalse())
		Ω(IsAWSSecretsManagerSecret(sn)).Should(BeFalse())
	})
})
