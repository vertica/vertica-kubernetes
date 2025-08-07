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

	vops "github.com/vertica/vcluster/vclusterops"

	"github.com/vertica/vertica-kubernetes/pkg/net"
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
	if err != nil {
		return fmt.Errorf("failed to poll https: %w", err)
	}
	return nil
}

func (v *VClusterOps) genPollHTTPSOptions(s *pollhttps.Parms,
	certs *HTTPSCerts) *vops.VPollHTTPSOptions {
	opts := vops.VPollHTTPSOptionsFactory()

	opts.Hosts = append(opts.Hosts, s.InitiatorIPs...)
	opts.DBName = v.VDB.Spec.DBName

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIPs[0])

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)
	if v.Password != "" {
		opts.Password = &v.Password
	}
	opts.MainClusterHosts = s.MainClusterHosts
	return &opts
}
