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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/pollscstate"
)

// PollSubclusterState waits for a subcluster to come up
func (v *VClusterOps) PollSubclusterState(ctx context.Context, opts ...pollscstate.Option) (err error) {
	v.setupForAPICall("PollSubclusterState")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster PollSubclusterState")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return
	}

	s := pollscstate.Params{}
	s.Make(opts...)

	vcOpts := v.genPollSubclusterStateOptions(&s, certs)
	err = v.VPollSubclusterState(vcOpts)
	if err != nil {
		return fmt.Errorf("subcluster polling failed: %w", err)
	}

	return
}

func (v *VClusterOps) genPollSubclusterStateOptions(s *pollscstate.Params, certs *HTTPSCerts) *vops.VPollSubclusterStateOptions {
	opts := vops.VPollSubclusterStateOptionsFactory()

	// required options
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIPs...)
	v.Log.Info("Setup poll sc state options", "hosts", opts.RawHosts)
	opts.IPv6 = net.IsIPv6(s.InitiatorIPs[0])

	opts.SCName = s.Subcluster

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
