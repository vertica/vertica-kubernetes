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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
)

// RotateNMACerts will rotate nma cert
func (v *VClusterOps) RotateNMACerts(ctx context.Context, opts ...rotatenmacerts.Option) error {
	v.setupForAPICall("RotateNMACerts")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RotateNMACerts")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := rotatenmacerts.Params{}
	s.Make(opts...)

	// call vclusterOps library to rotate nma cert
	vopts := v.genRotateNMACertsOptions(&s, certs)
	err = v.VRotateNMACerts(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rotate nma cert")
		return err
	}
	v.Log.Info("Successfully rotate nma cert")
	return nil
}

func (v *VClusterOps) genRotateNMACertsOptions(s *rotatenmacerts.Params, certs *HTTPSCerts) vops.VRotateNMACertsOptions {
	opts := vops.VRotateNMACertsOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.Hosts = s.Hosts

	opts.IPv6 = net.IsIPv6(s.Hosts[0])

	opts.NewClientTLSConfig = vops.NewClientTLSConfig{
		NewKey:    s.NewKey,
		NewCert:   s.NewCert,
		NewCaCert: s.NewCaCert,
	}
	opts.DoKillNMA = true
	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)
	return opts
}
