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
	"errors"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/unsandboxsc"
)

// UnsandboxSubcluster will move a subcluster from a sandbox to main cluster
func (v *VClusterOps) UnsandboxSubcluster(ctx context.Context, opts ...unsandboxsc.Option) error {
	v.setupForAPICall("UnsandboxSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster UnsandboxSubcluster")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}

	// get unsandbox_subcluster k8s configs
	s := unsandboxsc.Params{}
	s.Make(opts...)

	// call vclusterOps library to unsandbox a subcluster
	vopts := v.genUnsandboxSubclusterOptions(&s, certs)
	err = v.VUnsandbox(&vopts)
	if err != nil {
		scNotSandboxedError := &vops.SubclusterNotSandboxedError{}
		if ok := errors.As(err, &scNotSandboxedError); ok {
			v.Log.Info("Subcluster has already been unsandboxed, no need to unsandbox it again",
				"subcluster", vopts.SCName)
			return nil
		}
		v.Log.Error(err, "failed to unsandbox a subcluster", "subcluster", vopts.SCName)
		return err
	}

	v.Log.Info("Successfully unsandbox a subcluster", "subcluster", vopts.SCName)
	return nil
}

func (v *VClusterOps) genUnsandboxSubclusterOptions(s *unsandboxsc.Params, certs *tls.HTTPSCerts) vops.VUnsandboxOptions {
	opts := vops.VUnsandboxOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	if s.SBInitiatorIP != "" {
		opts.RawHosts = append(opts.RawHosts, s.SBInitiatorIP)
	}
	v.Log.Info("Setup unsandbox subcluster options", "hosts", opts.RawHosts)
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SCName = s.Subcluster
	opts.RestartSC = false
	opts.PrimaryUpHost = s.InitiatorIP
	opts.NodeNameAddressMap = s.NodeNameAddressMap

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	return opts
}
