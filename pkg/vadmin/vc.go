/*
 (c) Copyright [2021-2023] Open Text.
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

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// retrieveHTTPSCerts will retrieve the certs from HTTPServerTLSSecret for calling NMA endpoints
func (v VClusterOps) retrieveHTTPSCerts(ctx context.Context) (*HTTPSCerts, error) {
	certs := HTTPSCerts{}

	nm := types.NamespacedName{
		Namespace: v.VDB.Namespace,
		Name:      v.VDB.Spec.HTTPServerTLSSecret,
	}
	tlsCerts := &corev1.Secret{}
	err := v.Client.Get(ctx, nm, tlsCerts)
	if err != nil {
		return nil, err
	}

	tlsKey, ok := tlsCerts.Data[corev1.TLSPrivateKeyKey]
	if !ok {
		return nil, fmt.Errorf("key %s is missing in the secret %s", corev1.TLSPrivateKeyKey, tlsCerts.Name)
	}
	tlsCrt, ok := tlsCerts.Data[corev1.TLSCertKey]
	if !ok {
		return nil, fmt.Errorf("cert %s is missing in the secret %s", corev1.TLSCertKey, tlsCerts.Name)
	}
	tlsCaCrt, ok := tlsCerts.Data[corev1.ServiceAccountRootCAKey]
	if !ok {
		return nil, fmt.Errorf("ca cert %s is missing in the secret %s", corev1.ServiceAccountRootCAKey, tlsCerts.Name)
	}
	certs.Key = string(tlsKey)
	certs.Cert = string(tlsCrt)
	certs.CaCert = string(tlsCaCrt)

	return &certs, nil
}
