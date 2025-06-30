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
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatetlscerts"
)

const tlsConfigServer = "Server"

// RotateNMACerts will rotate nma cert
func (v *VClusterOps) RotateTLSCerts(ctx context.Context, opts ...rotatetlscerts.Option) error {
	v.setupForAPICall("RotateTLSCerts")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RotateTLSCerts")

	s := rotatetlscerts.Params{}
	s.Make(opts...)

	secretName := v.VDB.GetHTTPSTLSSecretNameInUse()
	if s.TLSConfig == tlsConfigServer && secretName != v.VDB.Spec.HTTPSNMATLSSecret {
		// https cert rotation has already occurred but the status is not up to date so
		// the cert in use is the one in the spec
		v.Log.Info("HTTPS cert rotation has occurred but the status is not up to date yet. Using secret from spec")
		secretName = v.VDB.Spec.HTTPSNMATLSSecret
	}
	// get the certs
	fetcher := cloud.SecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		Obj:      v.VDB,
		EVWriter: v.EVWriter,
	}
	certs, err := retrieveNMACerts(ctx, &fetcher, v.VDB, secretName)
	if err != nil {
		return err
	}

	// call vclusterOps library to rotate nma cert
	vopts := v.genRotateTLSCertsOptions(&s, certs)
	err = v.VRotateTLSCerts(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rotate tls cert")
		return err
	}
	v.Log.Info("Successfully rotate tls cert")
	return nil
}

func (v *VClusterOps) genRotateTLSCertsOptions(s *rotatetlscerts.Params, certs *HTTPSCerts) vops.VRotateTLSCertsOptions {
	opts := vops.VRotateTLSCertsOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup rotate tls cert options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.NewClientTLSConfig = vops.NewClientTLSConfig{
		NewKey:    s.NewKey,
		NewCert:   s.NewCert,
		NewCaCert: s.NewCaCert,
	}
	opts.NewSecretMetadata = vops.RotateTLSCertsData{
		KeySecretName:    s.KeySecretName,
		KeyConfig:        s.KeyConfig,
		CertSecretName:   s.CertSecretName,
		CertConfig:       s.CertConfig,
		CACertSecretName: s.CACertSecretName,
		CACertConfig:     s.CACertConfig,
		TLSMode:          s.TLSMode,
		TLSConfig:        s.TLSConfig,
	}
	opts.UserName = v.VDB.GetVerticaUser()
	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)
	opts.TLSSecretManager = s.NewSecretManager

	return opts
}
