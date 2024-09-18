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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/manageconnectiondraining"
)

// ManageConnectionDraining pauses/redirects/resumes connections on the vertica cluster
func (v *VClusterOps) ManageConnectionDraining(ctx context.Context, opts ...manageconnectiondraining.Option) error {
	v.setupForAPICall("ManageConnectionDraining")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster ManageConnectionDraining")

	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := manageconnectiondraining.Params{}
	s.Make(opts...)

	vcOpts := v.genManageConnectionDrainingOptions(&s, certs)
	err = v.VManageConnectionDraining(vcOpts)
	if err != nil {
		return fmt.Errorf("failed to get run connection draining operation: %w", err)
	}

	return nil
}

func (v *VClusterOps) genManageConnectionDrainingOptions(s *manageconnectiondraining.Params,
	certs *HTTPSCerts) *vops.VManageConnectionDrainingOptions {
	opts := vops.VManageConnectionDrainingOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	opts.DBName = v.VDB.Spec.DBName
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	opts.Sandbox = s.Sandbox
	opts.SCName = s.SCName
	opts.Action = s.Action
	opts.RedirectHostname = s.RedirectHostname

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
