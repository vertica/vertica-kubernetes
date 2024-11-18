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

	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// retrieveNMACerts will retrieve the certs from NMATLSSecret for calling NMA endpoints
func (v *VClusterOps) retrieveNMACerts(ctx context.Context) (*HTTPSCerts, error) {
	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		VDB:      v.VDB,
		EVWriter: v.EVWriter,
	}
	return retrieveNMACerts(ctx, fetcher)
}

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
