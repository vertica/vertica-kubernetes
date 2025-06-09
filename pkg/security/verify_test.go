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

package security

import (
	"crypto/x509"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("certificate validation", func() {
	var caCert Certificate

	BeforeEach(func() {
		var err error
		caCert, err = NewSelfSignedCACertificate()
		Expect(err).Should(Succeed())
	})

	It("should pass validation for cert with correct EKU and date", func() {
		cert, err := NewCertificate(caCert, "valid", []string{"valid.example.com"})
		Expect(err).Should(Succeed())
		Expect(ValidateCertificate(cert.TLSCrt())).Should(Succeed())

		// Check that it is not expiring soon
		expiringSoon, _, err := CheckCertificateExpiringSoon(cert.TLSCrt())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(expiringSoon).Should(BeFalse())

		// Check SKI for CA cert
		Expect(ValidateCertificate(caCert.TLSCrt())).Should(Succeed())
	})

	It("should fail validation if EKU is missing clientAuth", func() {
		cert, err := NewTestCertificate(
			caCert,
			"missing-client-auth",
			[]string{"clientless.example.com"},
			nil,
			time.Now(),
			time.Now().Add(365*24*time.Hour),
			[]x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
			x509.KeyUsageDigitalSignature,
			false,
		)
		Expect(err).Should(Succeed())
		Expect(ValidateCertificate(cert.TLSCrt())).Should(MatchError(ContainSubstring("clientAuth")))
	})

	It("should fail validation if EKU is missing serverAuth", func() {
		cert, err := NewTestCertificate(
			caCert,
			"missing-server-auth",
			[]string{"serverless.example.com"},
			nil,
			time.Now(),
			time.Now().Add(365*24*time.Hour),
			[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
			x509.KeyUsageDigitalSignature,
			false,
		)
		Expect(err).Should(Succeed())
		Expect(ValidateCertificate(cert.TLSCrt())).Should(MatchError(ContainSubstring("serverAuth")))
	})

	It("should fail validation for expired cert", func() {
		cert, err := NewTestCertificate(
			caCert,
			"expired",
			[]string{"expired.example.com"},
			nil,
			time.Now().Add(-2*24*time.Hour),
			time.Now().Add(-24*time.Hour),
			[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			x509.KeyUsageDigitalSignature,
			false,
		)
		Expect(err).Should(Succeed())
		Expect(ValidateCertificate(cert.TLSCrt())).Should(MatchError(ContainSubstring("expired")))
	})

	It("should fail validation for cert not yet valid", func() {
		cert, err := NewTestCertificate(
			caCert,
			"future",
			[]string{"future.example.com"},
			nil,
			time.Now().Add(24*time.Hour),
			time.Now().Add(365*24*time.Hour),
			[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			x509.KeyUsageDigitalSignature,
			false,
		)
		Expect(err).Should(Succeed())
		Expect(ValidateCertificate(cert.TLSCrt())).Should(MatchError(ContainSubstring("not valid yet")))
	})

	It("should return true for certs expiring within renewal threshold", func() {
		cert, err := NewTestCertificate(
			caCert,
			"expiring-soon",
			[]string{"soon.example.com"},
			nil,
			time.Now().Add(-1*time.Hour),
			time.Now().Add(renewalThreshold/2), // Expiring soon
			[]x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
			x509.KeyUsageDigitalSignature,
			false,
		)
		Expect(err).Should(Succeed())

		expiringSoon, _, err := CheckCertificateExpiringSoon(cert.TLSCrt())
		Expect(err).ShouldNot(HaveOccurred())
		Expect(expiringSoon).Should(BeTrue())
	})
})
