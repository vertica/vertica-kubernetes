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
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
)

// RotateNMACerts will rotate nma cert
func (v *VClusterOps) RotateHTTPSCerts(ctx context.Context, opts ...rotatehttpscerts.Option) error {
	v.setupForAPICall("RotateHTTPSCerts")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RotateHTTPSCerts")
	secretName := v.VDB.GetHTTPSTLSSecretNameInUse()
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

	s := rotatehttpscerts.Params{}
	s.Make(opts...)

	// call vclusterOps library to rotate nma cert
	vopts := v.genRotateHTTPSCertsOptions(&s, certs)
	err = v.VRotateTLSCerts(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rotate https cert")
		return err
	}
	v.Log.Info("Successfully rotate https cert")
	return nil
}

func (v *VClusterOps) genRotateHTTPSCertsOptions(s *rotatehttpscerts.Params, certs *HTTPSCerts) vops.VRotateTLSCertsOptions {
	opts := vops.VRotateTLSCertsOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup rotate https cert options", "hosts", opts.RawHosts[0])
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
		TLSConfig:        "HTTP",
	}
	opts.UserName = v.VDB.GetVerticaUser()
	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)
	secretManager := ""
	switch {
	case secrets.IsAWSSecretsManagerSecret(v.VDB.Spec.NMATLSSecret):
		secretManager = vops.AWSSecretManagerType
	case secrets.IsK8sSecret(v.VDB.Spec.NMATLSSecret):
		secretManager = vops.K8sSecretManagerType
	}
	opts.TLSSecretManager = secretManager
	return opts
}
