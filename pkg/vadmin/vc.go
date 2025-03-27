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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// retrieveNMACerts will retrieve the certs from NMATLSSecret for calling NMA endpoints
func (v *VClusterOps) retrieveNMACerts(_ context.Context) (*HTTPSCerts, error) {
	fetcher := cloud.SecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		Obj:      v.VDB,
		EVWriter: v.EVWriter,
	}
	secretName, err := getNMATLSSecretName(v.VDB)
	if err != nil {
		v.Log.Error(err, "failed to get nma secret name")
		return nil, err
	}
	httpCerts, err2 := getCertFromSecret(v.VDB.Namespace, secretName, fetcher)
	if err2 != nil {
		v.Log.Error(err2, "failed to get cert from secret")
	}
	v.Log.Info("nma secret name " + secretName + ", cert " + httpCerts.Cert)
	return httpCerts, err2
}

// retrieveTargetNMACerts will retrieve the certs from NMATLSSecret for calling target NMA endpoints
func (v *VClusterOps) retrieveTargetNMACerts(ctx context.Context) (*HTTPSCerts, error) {
	fetcher := cloud.SecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		Obj:      v.TargetVDB,
		EVWriter: v.EVWriter,
	}
	return retrieveNMACerts(ctx, &fetcher, v.TargetVDB)
}

func retrieveNMACerts(ctx context.Context, fetcher *cloud.SecretFetcher, vdb *vapi.VerticaDB) (*HTTPSCerts, error) {
	tlsCerts, err := fetcher.Fetch(ctx, names.GenNamespacedName(vdb, vdb.Spec.NMATLSSecret))
	if err != nil {
		return nil, fmt.Errorf("fetching NMA certs: %w", err)
	}

	tlsKey, ok := tlsCerts[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("key %s is missing in the secret %s", corev1.TLSPrivateKeyKey, vdb.Spec.NMATLSSecret)
	}
	tlsCrt, ok := tlsCerts[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("cert %s is missing in the secret %s", corev1.TLSCertKey, vdb.Spec.NMATLSSecret)
	}
	tlsCaCrt, ok := tlsCerts[corev1.ServiceAccountRootCAKey]
	if !ok {
		return nil, fmt.Errorf("ca cert %s is missing in the secret %s", corev1.ServiceAccountRootCAKey, vdb.Spec.NMATLSSecret)
	}
	return &HTTPSCerts{
		Key:    string(tlsKey),
		Cert:   string(tlsCrt),
		CaCert: string(tlsCaCrt),
	}, nil
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

func (v *VClusterOps) setAuthentication(opts *vops.DatabaseOptions, username string, password *string, certs *HTTPSCerts) {
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	if !v.VDB.IsCertRotationEnabled() {
		opts.UserName = username
		opts.Password = password
	}
}

// getNMATLSSecretName returns the name of the secret that stores TLS cert
// when tls cert is NOT used, it returns vdb.Spec.NMATLSSecret. This includes
// the time before a vdb is created
// when tls cert is used, it returns secert name saved in annotation
func getNMATLSSecretName(vdb *vapi.VerticaDB) (string, error) {

	secretName := ""
	if vdb.IsCertRotationEnabled() && vdb.IsStatusConditionTrue(vapi.DBInitialized) {
		secretName = meta.GetNMATLSSecretNameInUse(vdb.Annotations)
	} else {
		secretName = vdb.Spec.NMATLSSecret
	}
	if secretName == "" {
		return "", fmt.Errorf("failed to retrieve nma secret name")
	}
	return secretName, nil
}

// getCertFromSecret will read secret from the secret name and return a cert
func getCertFromSecret(namespace, secretName string, fetcher cloud.SecretFetcher) (*HTTPSCerts, error) {
	secretMap, err := retrieveSecretFromName(namespace, secretName, fetcher)
	if err != nil {
		return nil, err // failed to load secret
	}
	return &HTTPSCerts{
		Key:    string(secretMap[corev1.TLSPrivateKeyKey]),
		Cert:   string(secretMap[corev1.TLSCertKey]),
		CaCert: string(secretMap[paths.HTTPServerCACrtName]),
	}, nil
}

// retrieveSecretByName loads secret from k8s by secret name
func retrieveSecretFromName(namespace, secretName string, fetcher cloud.SecretFetcher) (map[string][]byte, error) {
	ctx := context.Background()
	fetchName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}
