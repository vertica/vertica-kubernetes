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
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/pollhttps"
)

//nolint:dupl
func (v *VClusterOps) PollHTTPS(ctx context.Context, opts ...pollhttps.Option) error {
	v.setupForAPICall("PollHttps")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster PollHTTPS")
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}
	s := pollhttps.Parms{}
	s.Make(opts...)
	vcOpts := v.genPollHTTPSOptions(&s, certs)
	err = v.VPollHTTPS(vcOpts)

	// This annotation is only for testing purposes to simulate a failure
	triggeredFailure := strings.Split(vmeta.GetTriggerTLSUpdateFailureAnnotation(v.VDB.Annotations), ",")[0]
	if err == nil && triggeredFailure == vmeta.TriggerTLSUpdateFailureAfterHTTPSTLSUpdate {
		err = fmt.Errorf("injected failure during HTTPS polling")
	}

	if err != nil {
		return fmt.Errorf("failed to poll https: %w", err)
	}
	return nil
}

func (v *VClusterOps) genPollHTTPSOptions(s *pollhttps.Parms,
	certs *tls.HTTPSCerts) *vops.VPollHTTPSOptions {
	opts := vops.VPollHTTPSOptionsFactory()

	opts.Hosts = append(opts.Hosts, s.InitiatorIPs...)
	opts.DBName = v.VDB.Spec.DBName

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIPs[0])

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), v.Password, certs)
	opts.MainClusterInitiator = s.MainClusterInitiator
	opts.NewKey = s.NewKey
	opts.NewCert = s.NewCert
	opts.NewCaCert = s.NewCaCert
	opts.SyncCatalogRequired = s.SyncCatalogRequire
	return &opts
}
