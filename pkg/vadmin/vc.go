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

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// retrieveTargetNMACerts will retrieve the certs from NMATLSSecret for calling target NMA endpoints
func (v *VClusterOps) retrieveTargetNMACerts(ctx context.Context) (*HTTPSCerts, error) {
	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		VDB:      v.TargetVDB,
		EVWriter: v.EVWriter,
	}
	return retrieveNMACerts(ctx, fetcher)
}

// retrieveNMACerts will retrieve the certs from NMATLSSecret for calling NMA endpoints
func (v *VClusterOps) retrieveNMACerts(_ context.Context) (*HTTPSCerts, error) {
	vdbContext := GetContextForVdb(v.VDB.Namespace, v.VDB.Name)
	namSecretName, err := getNMATLSSecretName(v.VDB)
	if err != nil {
		return nil, err
	}
	v.Log.Info("libo: nma secret name to use " + namSecretName)
	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		VDB:      v.VDB,
		EVWriter: v.EVWriter,
	}
	return vdbContext.GetCertFromSecret(namSecretName, fetcher)
}

func retrieveNMACerts(ctx context.Context, fetcher cloud.VerticaDBSecretFetcher) (*HTTPSCerts, error) {
	tlsCerts, err := fetcher.Fetch(ctx, names.GenNamespacedName(fetcher.VDB, fetcher.VDB.Spec.NMATLSSecret))
	if err != nil {
		return nil, fmt.Errorf("fetching NMA certs: %w", err)
	}

	tlsKey, ok := tlsCerts[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("key %s is missing in the secret %s", corev1.TLSPrivateKeyKey, fetcher.VDB.Spec.NMATLSSecret)
	}
	tlsCrt, ok := tlsCerts[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("cert %s is missing in the secret %s", corev1.TLSCertKey, fetcher.VDB.Spec.NMATLSSecret)
	}
	tlsCaCrt, ok := tlsCerts[corev1.ServiceAccountRootCAKey]
	if !ok {
		return nil, fmt.Errorf("ca cert %s is missing in the secret %s", corev1.ServiceAccountRootCAKey, fetcher.VDB.Spec.NMATLSSecret)
	}
	return &HTTPSCerts{
		Key:    string(tlsKey),
		Cert:   string(tlsCrt),
		CaCert: string(tlsCaCrt),
	}, nil
}

// getNMATLSSecretName returns the name of the secret that stores TLS cert
// when tls cert is NOT used, it returns vdb.Spec.NMATLSSecret. This includes
// the time before a vdb is created
// when tls cert is used, it returns secert name saved in annotation
func getNMATLSSecretName(vdb *vapi.VerticaDB) (string, error) {
	vdbContext := GetContextForVdb(vdb.Namespace, vdb.Name)
	secretName := ""
	if vdbContext.GetBoolValue(UseTLSCert) {
		secretName = meta.GetNMATLSSecretNameInUse(vdb.Annotations)
	} else {
		secretName = vdb.Spec.NMATLSSecret
	}
	if secretName == "" {
		return "", fmt.Errorf("failed to retrieve nma secret name")
	}
	return secretName, nil
}

// logFailure will log and record an event for a vclusterOps API failure
func (v *VClusterOps) logFailure(cmd, genericFailureReason string, err error) (ctrl.Result, error) {
	evLogr := vcErrors{
		VDB:                  v.VDB,
		Log:                  v.Log,
		GenericFailureReason: genericFailureReason,
		EVWriter:             v.EVWriter,
	}
	return evLogr.LogFailure(cmd, err)
}

// shouldUseCertAuthentication returns true when tls cert is used
func (v *VClusterOps) shouldUseCertAuthentication() bool {
	Vinf, ok := v.VDB.MakeVersionInfo()
	if !ok {
		v.Log.Info("failed to get vertica version. disable TLS cert")
		return false
	}
	if Vinf.IsEqualOrNewer(vapi.NMATLSCertRotationMinVersion) && meta.EnableTLSCertsRotation(v.VDB.Annotations) {
		vdbContext := GetContextForVdb(v.VDB.Namespace, v.VDB.Name)
		return vdbContext.GetBoolValue(UseTLSCert)
	}
	return false
}
