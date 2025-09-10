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
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/pollhttps"
)

// mock version of VPollHTTPS() that is invoked inside VClusterOps.VPollHTTPS()
func (m *MockVClusterOps) VPollHTTPS(options *vops.VPollHTTPSOptions) (err error) {
	if options.NewKey != test.TestKeyValueTwo || options.NewCert != test.TestCertValueTwo ||
		options.NewCaCert != test.TestCaCertValueTwo {
		return fmt.Errorf("new cert fields don't match")
	}
	if options.MainClusterInitiator != "192.168.0.1,192.168.0.3" {
		return fmt.Errorf("main cluster hosts are not passed properly")
	}
	if !slices.Equal(options.Hosts, []string{"192.168.0.1", "192.168.0.2", "192.168.0.3"}) {
		return fmt.Errorf("initiators are not passed properly")
	}
	if !options.SyncCatalogRequired {
		return fmt.Errorf("SyncCatalogRequired is not passed properly")
	}
	return nil
}

var _ = Describe("poll_https_vc", func() {
	ctx := context.Background()

	It("should pass parameters to VPollHTTPS() as expected", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.HTTPSNMATLS.Secret = "poll-https-vc-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.HTTPSNMATLS.Secret)

		certs := &tls.HTTPSCerts{}
		certs.Key = test.TestKeyValueTwo
		certs.Cert = test.TestCertValueTwo
		certs.CaCert = test.TestCaCertValueTwo
		err := dispatcher.PollHTTPS(ctx, pollhttps.WithInitiators([]string{"192.168.0.1", "192.168.0.2", "192.168.0.3"}),
			pollhttps.WithMainClusterHosts("192.168.0.1,192.168.0.3"),
			pollhttps.WithSyncCatalogRequired(true), pollhttps.WithNewHTTPSCerts(certs))
		Ω(err).Should(Succeed())
	})
})
