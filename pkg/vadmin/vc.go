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
	"regexp"
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// retrieveNMACerts will retrieve the certs from NMATLSSecret for calling NMA endpoints
func (v *VClusterOps) retrieveNMACerts(ctx context.Context) (*HTTPSCerts, error) {
	return v.retrieveNMACertsWithTarget(ctx, false)
}

// retrieveTargetNMACerts will retrieve the certs from NMATLSSecret for calling target NMA endpoints
func (v *VClusterOps) retrieveTargetNMACerts(ctx context.Context) (*HTTPSCerts, error) {
	return v.retrieveNMACertsWithTarget(ctx, true)
}

func (v *VClusterOps) retrieveNMACertsWithTarget(ctx context.Context, forTarget bool) (*HTTPSCerts, error) {
	vdb := v.VDB
	if forTarget {
		vdb = v.TargetVDB
	}

	// Determine the secret name
	secretName, err := getNMATLSSecretName(vdb)
	if err != nil {
		v.Log.Error(err, "failed to get nma secret name")
		return nil, err
	}

	v.Log.Info("nma secret name used - " + secretName)

	fetcher := cloud.SecretFetcher{
		Client:   v.Client,
		Log:      v.Log,
		Obj:      vdb,
		EVWriter: v.EVWriter,
	}

	return retrieveNMACerts(ctx, &fetcher, vdb, secretName)
}

func retrieveNMACerts(ctx context.Context, fetcher *cloud.SecretFetcher, vdb *vapi.VerticaDB, secretName string) (*HTTPSCerts, error) {
	tlsCerts, err := fetcher.Fetch(ctx, names.GenNamespacedName(vdb, secretName))
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

func genTLSConfigurationMap(tlsMode, secretNameInVdb, secretNamespace string) map[string]string {
	configMap := make(map[string]string)
	configMap[vops.TLSSecretManagerKeyCACertDataKey] = corev1.ServiceAccountRootCAKey
	configMap[vops.TLSSecretManagerKeyCertDataKey] = corev1.TLSCertKey
	configMap[vops.TLSSecretManagerKeyKeyDataKey] = corev1.TLSPrivateKeyKey
	secretName := secretNameInVdb
	secretManager := ""
	switch {
	case secrets.IsGSMSecret(secretNameInVdb):
		return configMap
	case secrets.IsAWSSecretsManagerSecret(secretNameInVdb):
		region, _ := secrets.GetAWSRegion(secretNameInVdb)
		configMap[vops.TLSSecretManagerKeyAWSRegion] = region
		secretARN, versionID := secrets.GetAWSSecretARN(secretNameInVdb)
		configMap[vops.TLSSecretManagerKeyAWSSecretVersionID] = versionID
		secretName = secretARN
		secretManager = vops.AWSSecretManagerType
	default:
		secretManager = vops.K8sSecretManagerType
		configMap[vops.TLSSecretManagerKeyNamespace] = secretNamespace
	}
	configMap[vops.TLSSecretManagerKeySecretManager] = secretManager
	configMap[vops.TLSSecretManagerKeySecretName] = secretName
	configMap[vops.TLSSecretManagerKeyTLSMode] = strings.ToLower(genVclusteropsCompatibleTLSMode(tlsMode))

	return configMap
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
		secretName = vdb.GetNMATLSSecretNameInUse()
	} else {
		secretName = vdb.Spec.NMATLSSecret
	}
	if secretName == "" {
		return "", fmt.Errorf("failed to retrieve nma secret name")
	}
	return secretName, nil
}

func genVclusteropsCompatibleTLSMode(tlsMode string) string {
	m := regexp.MustCompile(`_`)
	return m.ReplaceAllString(tlsMode, "-")
}
