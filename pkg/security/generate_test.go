/*
 (c) Copyright [2021-2022] Open Text.
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

package security

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("security", func() {
	It("generate a cert with no dns names", func() {
		caCert, err := NewSelfSignedCACertificate(512)
		Expect(err).Should(Succeed())
		verifyCerts(NewCertificate(caCert, 512, "dbadmin", nil))
	})

	It("generate certs with multiple DNS names", func() {
		caCert, err := NewSelfSignedCACertificate(512)
		Expect(err).Should(Succeed())
		verifyCerts(NewCertificate(caCert, 512, "dbadmin", []string{"host1", "host2"}))
	})
})

func verifyCerts(cert Certificate, err error) {
	ExpectWithOffset(1, err).Should(Succeed())
	ExpectWithOffset(1, len(cert.TLSCrt())).ShouldNot(Equal(0))
	ExpectWithOffset(1, len(cert.TLSKey())).ShouldNot(Equal(0))
}
