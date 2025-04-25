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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
)

const (
	rotateNmaCertNewNMASecretName     = "rotate-nma-new-cert-test-secret"     //nolint:gosec
	rotateNmaCertCurrentNMASecretName = "rotate-nma-current-cert-test-secret" //nolint:gosec
)

// mock version of VRotateHTTPSCerts() that is invoked inside VClusterOps.RotateHTTPSCerts()
func (m *MockVClusterOps) VRotateNMACerts(options *vops.VRotateNMACertsOptions) error {
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return err
	}

	// verify hosts and eon mode
	if !(len(options.Hosts) != 0 && options.Hosts[0] == TestInitiatorIP) {
		return fmt.Errorf("failed to retrieve hosts")
	}

	if options.NewClientTLSConfig.NewKey != TestPollingKey {
		return fmt.Errorf("new key is not passed properly")
	}
	if options.NewClientTLSConfig.NewCert != TestPollingCert {
		return fmt.Errorf("new cert is not passed properly")
	}
	if options.NewClientTLSConfig.NewCaCert != TestPollingCaCert {
		return fmt.Errorf("new ca cert is not passed properly")
	}
	return nil
}

var _ = Describe("rotate_https_cert", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with rotate_nma_cert task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.NMATLSSecret = rotateNmaCertNewNMASecretName
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, rotateNmaCertCurrentNMASecretName)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		dispatcher.VDB.Spec.DBName = TestDBName
		vapi.SetVDBForTLS(dispatcher.VDB)
		dispatcher.VDB.Status.SecretRefs = []vapi.SecretRef{
			{
				Name: rotateHTTPSCertCurrentNMASecretName,
				Type: vapi.NMATLSSecretType,
			},
		}
		hosts := []string{TestInitiatorIP}
		Ω(dispatcher.RotateNMACerts(ctx,
			rotatenmacerts.WithKey(TestPollingKey),
			rotatenmacerts.WithCert(TestPollingCert),
			rotatenmacerts.WithCaCert(TestPollingCaCert),
			rotatenmacerts.WithHosts(hosts),
		)).Should(Succeed())
	})
})
