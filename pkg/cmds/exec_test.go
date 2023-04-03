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

package cmds

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("exec", func() {
	It("should obfuscate password", func() {
		s := generateLogOutput("vsql", "--password", "silly", "-c", "select 1")
		Expect(s).Should(Equal("vsql --password ******* -c select 1 "))
	})

	It("should obfuscate aws credentials", func() {
		s := generateLogOutput(`cat > auth_parms.conf<<< '
awsauth = user:pass
awsendpoint = minio`)
		Expect(s).Should(Equal("cat > auth_parms.conf<<< '\nawsauth = ****\nawsendpoint = minio "))
	})

	It("should obfuscate gcs credentials", func() {
		s := generateLogOutput(`cat > auth_parms.conf<<< '
GCSAuth = user:pass
GCSEndpoint = google`)
		Expect(s).Should(Equal("cat > auth_parms.conf<<< '\nGCSAuth = ****\nGCSEndpoint = google "))
	})

	It("should obfuscate azure credentials", func() {
		s := generateLogOutput(`cat > auth_parms.conf<<< '
AzureStorageCredentials = {"elem1": "a", "elem2": "b"}`)
		Expect(s).Should(Equal("cat > auth_parms.conf<<< '\nAzureStorageCredentials = **** "))
	})
})
