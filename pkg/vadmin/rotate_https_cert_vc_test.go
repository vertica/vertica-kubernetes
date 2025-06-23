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

package vadmin

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
)

const (
	rotateHTTPSCertNewNMASecretName     = "rotate-https-new-cert-test-secret"     //nolint:gosec
	rotateHTTPSCertCurrentNMASecretName = "rotate-https-current-cert-test-secret" //nolint:gosec
)

// mock version of VRotateTLSCerts() that is invoked inside VClusterOps.RotateHTTPSCerts()
func (m *MockVClusterOps) VRotateTLSCerts(options *vops.VRotateTLSCertsOptions) error {
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify hosts and eon mode
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	if options.NewSecretMetadata.KeySecretName != rotateHTTPSCertNewNMASecretName {
		return fmt.Errorf("new key secret is not passed properly")
	}
	if options.NewSecretMetadata.CertSecretName != rotateHTTPSCertNewNMASecretName {
		return fmt.Errorf("new cert secret is not passed properly")
	}
	if options.NewSecretMetadata.CACertSecretName != rotateHTTPSCertNewNMASecretName {
		return fmt.Errorf("new ca cert secret is not passed properly")
	}

	if options.NewSecretMetadata.KeyConfig != TestKeyConfig {
		return fmt.Errorf("new key config is not passed properly")
	}
	if options.NewSecretMetadata.CertConfig != TestCertConfig {
		return fmt.Errorf("new cert config is not passed properly")
	}
	if options.NewSecretMetadata.CACertConfig != TestCaCertConfig {
		return fmt.Errorf("new ca cert config is not passed properly")
	}

	if options.NewClientTLSConfig.NewCaCert != TestPollingCaCert {
		return fmt.Errorf("new ca cert is not passed properly")
	}
	if options.NewClientTLSConfig.NewCert != TestPollingCert {
		return fmt.Errorf("new cert is not passed properly")
	}
	if options.NewClientTLSConfig.NewKey != TestPollingKey {
		return fmt.Errorf("new key is not passed properly")
	}

	if options.NewSecretMetadata.TLSMode != "TRY_VERIFY" {
		return fmt.Errorf("tls mode is not passed properly")
	}

	return nil
}

var _ = Describe("rotate_https_cert", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with rotate_https_cert task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = rotateHTTPSCertNewNMASecretName
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, rotateHTTPSCertCurrentNMASecretName)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Status.TLSConfig = []vapi.TLSConfig{
			{
				Secret: rotateHTTPSCertCurrentNMASecretName,
				Name:   vapi.HTTPSTLSSecretType,
			},
		}
		Ω(dispatcher.RotateHTTPSCerts(ctx,
			rotatehttpscerts.WithInitiator(TestInitiatorIP),
			rotatehttpscerts.WithPollingKey(TestPollingKey),
			rotatehttpscerts.WithPollingCert(TestPollingCert),
			rotatehttpscerts.WithPollingCaCert(TestPollingCaCert),
			rotatehttpscerts.WithKey(dispatcher.VDB.Spec.HTTPSNMATLS.Secret, TestKeyConfig),
			rotatehttpscerts.WithCert(dispatcher.VDB.Spec.HTTPSNMATLS.Secret, TestCertConfig),
			rotatehttpscerts.WithCaCert(dispatcher.VDB.Spec.HTTPSNMATLS.Secret, TestCaCertConfig),
			rotatehttpscerts.WithTLSMode("TRY_VERIFY"),
		)).Should(Succeed())
	})
})
