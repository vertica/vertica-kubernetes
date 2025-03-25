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
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
)

// RotateNMACerts will rotate nma cert
//
//nolint:dupl
func (v *VClusterOps) RotateHTTPSCerts(ctx context.Context, opts ...rotatehttpscerts.Option) error {
	v.setupForAPICall("RotateHTTPSCerts")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RotateHTTPSCerts")

	// get the certs
	secretName := meta.GetNMATLSSecretNameInUse(v.VDB.Annotations)
	vdbContext := GetContextForVdb(v.VDB.Namespace, v.VDB.Name)
	fetcher := cloud.SecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		Obj:      v.VDB,
		EVWriter: v.EVWriter,
	}
	certs, err := vdbContext.GetCertFromSecret(secretName, fetcher)
	/* certs, err := v.retrieveNMACerts(ctx) */
	if err != nil {
		return err
	}

	s := rotatehttpscerts.Params{}
	s.Make(opts...)

	// call vclusterOps library to rotate nma cert
	vopts := v.genRotateHTTPSCertsOptions(&s, certs)
	err = v.VRotateHTTPSCerts(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rotate https cert")
		return err
	}
	v.Log.Info("Successfully rotate https cert")
	return nil
}

func (v *VClusterOps) genRotateHTTPSCertsOptions(s *rotatehttpscerts.Params, certs *HTTPSCerts) vops.VRotateHTTPSCertsOptions {
	opts := vops.VRotateHTTPSCertsOptionsFactory()

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
	opts.NewSecretMetadata = vops.RotateHTTPSCertsData{
		KeySecretName:    s.KeySecretName,
		KeyConfig:        s.KeyConfig,
		CertSecretName:   s.CertSecretName,
		CertConfig:       s.CertConfig,
		CACertSecretName: s.CACertSecretName,
		CACertConfig:     s.CACertConfig,
		TLSMode:          s.TLSMode,
	}

	// auth options
	if v.shouldUseCertAuthentication() {
		opts.Key = certs.Key
		opts.Cert = certs.Cert
		opts.CaCert = certs.CaCert
		opts.UserName = v.VDB.GetVerticaUser()
	} else {
		opts.Key = certs.Key
		opts.Cert = certs.Cert
		opts.CaCert = certs.CaCert
		opts.UserName = v.VDB.GetVerticaUser()
		opts.Password = &v.Password
	}
	return opts
}
