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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/promotesandboxtomain"
)

// PromoteSandboxToMain can convert local sandbox to main cluster
func (v *VClusterOps) PromoteSandboxToMain(ctx context.Context, opts ...promotesandboxtomain.Option) (err error) {
	v.setupForAPICall("PromoteSandboxToMain")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster PromoteSandboxToMain")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := promotesandboxtomain.Params{}
	s.Make(opts...)

	vcOpts := v.genPromoteSandboxToMainOptions(&s, certs)
	err = v.VPromoteSandboxToMain(vcOpts)
	if err != nil {
		return fmt.Errorf("failed to promote sandbox to main: %w", err)
	}

	return nil
}

func (v *VClusterOps) genPromoteSandboxToMainOptions(s *promotesandboxtomain.Params, certs *HTTPSCerts) *vops.VPromoteSandboxToMainOptions {
	opts := vops.VPromoteSandboxToMainFactory()

	// required options
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup promote sandbox to main options", "hosts", opts.RawHosts)
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SandboxName = s.Sandbox

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
