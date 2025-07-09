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
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
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

	secretName := v.VDB.GetHTTPSNMATLSSecretInUse()
	if s.TLSConfig == tlsConfigServer && secretName != v.VDB.GetHTTPSNMATLSSecret() {
		// https cert rotation has already occurred but the status is not up to date so
		// the cert in use is the one in the spec
		v.Log.Info("HTTPS cert rotation has occurred but the status is not up to date yet. Using secret from spec")
		secretName = v.VDB.GetHTTPSNMATLSSecret()
	}
	certCache := v.CacheManager.GetCertCacheForVdb(v.VDB.Namespace, v.VDB.Name)
	certs, err := certCache.ReadCertFromSecret(ctx, secretName)
	if err != nil {
		return err
	}

	// In order to test TLS rollback after failed rotate, this is a backdoor set via
	// annotation to force a failure BEFORE the TLS cert has been updated in the DB
	if vmeta.GetTriggerTLSUpdateFailureAnnotation(v.VDB.Annotations) == vmeta.TriggerTLSUpdateFailureBeforeTLSUpdate {
		return fmt.Errorf("forced error in TLS cert rotation before updating TLS config")
	}

	// call vclusterOps library to rotate nma cert
	vopts := v.genRotateTLSCertsOptions(&s, certs)
	err = v.VRotateTLSCerts(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rotate tls cert")
		return err
	}

	// In order to test TLS rollback after failed rotate, this is a backdoor set via
	// annotation to force a failure AFTER the TLS cert has been updated in the DB
	if vmeta.GetTriggerTLSUpdateFailureAnnotation(v.VDB.Annotations) == vmeta.TriggerTLSUpdateFailureAfterTLSUpdate {
		return fmt.Errorf("forced error in TLS cert rotation after updating TLS config")
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
