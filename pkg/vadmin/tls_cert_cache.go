package vadmin

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ( // iota is reset to 0
	NmaTLSSecret = iota
	HTTPSTLSSecret
	ClientTLSSecret
)

var CertFields = map[string]bool{
	corev1.TLSPrivateKeyKey:   true,
	corev1.TLSCertKey:         true,
	paths.HTTPServerCACrtName: true,
}

type TLSCertCache struct {
	client.Client
	Log          logr.Logger
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	certCacheMap map[string]map[string][]byte
}

var tlsCertCacheManager *TLSCertCache

func TLSCertCacheFactory(cli client.Client, log logr.Logger,
	vdb *vapi.VerticaDB) *TLSCertCache {
	if tlsCertCacheManager == nil {
		tlsCertCacheManager = makeTLSCertCache(cli, log, vdb)
	}
	return tlsCertCacheManager
}

func makeTLSCertCache(cli client.Client, log logr.Logger,
	vdb *vapi.VerticaDB) *TLSCertCache {
	return &TLSCertCache{
		Client:       cli,
		Log:          log.WithName("TLSCertCache"),
		Vdb:          vdb,
		certCacheMap: map[string]map[string][]byte{},
	}
}

func (c *TLSCertCache) GetTLSPrivateKeyBytes(secret int) ([]byte, error) {
	return c.getTLSCertField(secret, corev1.TLSPrivateKeyKey)
}

func (c *TLSCertCache) GetTLSCertBytes(secret int) ([]byte, error) {
	return c.getTLSCertField(secret, corev1.TLSCertKey)
}

func (c *TLSCertCache) GetTLSCaCertBytes(secret int) ([]byte, error) {
	return c.getTLSCertField(secret, paths.HTTPServerCACrtName)
}

func (c *TLSCertCache) HasCert(secretName string) bool {
	_, ok := c.certCacheMap[secretName]
	return ok
}

func (c *TLSCertCache) GetHTTPSCerts(secret int) (*HTTPSCerts, error) {
	keyBytes, err := c.getTLSCertField(secret, corev1.TLSPrivateKeyKey)
	if err != nil {
		return nil, err
	}
	c.Log.Info("libo: key string - " + string(keyBytes))
	certBytes, _ := c.getTLSCertField(secret, corev1.TLSCertKey)
	caCertBytes, _ := c.getTLSCertField(secret, paths.HTTPServerCACrtName)

	return &HTTPSCerts{
		Key:    string(keyBytes),
		Cert:   string(certBytes),
		CaCert: string(caCertBytes),
	}, nil
}

func (c *TLSCertCache) getTLSCertField(secret int, fieldName string) ([]byte, error) {
	_, ok := CertFields[fieldName]
	if !ok {
		return nil, fmt.Errorf("invalid secret field name: %s", fieldName)
	}
	secretName, err := c.getSecretName(secret)
	if err != nil {
		return nil, fmt.Errorf("invalid secret name index: %d", secret)
	}

	secretMap, ok := c.certCacheMap[secretName]
	if ok {
		return secretMap[secretName], nil
	}
	c.Log.Info(secretName + " not found in cache. load it")
	// not found in cache. load from secretes
	secretMap, err = c.retrieveSecret(secret)
	if err != nil {
		c.Log.Error(err, "failed to load secret "+secretName)
		return nil, err
	}
	for field := range CertFields {
		_, ok := secretMap[field]
		if !ok {
			return nil, fmt.Errorf("secret %s is missing field: %s", secretName, field)
		}
	}
	c.certCacheMap[secretName] = secretMap
	return secretMap[secretName], nil
}

func (c *TLSCertCache) retrieveSecret(secret int) (map[string][]byte, error) {
	secretName, err := c.getSecretName(secret)
	if err != nil {
		return nil, err
	}
	fetcher := secrets.MultiSourceSecretFetcher{
		Log: &c.Log,
	}
	ctx := context.Background()
	fetchName := types.NamespacedName{
		Namespace: c.Vdb.GetObjectMeta().GetNamespace(),
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}

func (c *TLSCertCache) getSecretName(secret int) (string, error) {
	secretName := ""
	if secret == NmaTLSSecret {
		secretName = c.Vdb.Spec.NMATLSSecret
	} else if secret == ClientTLSSecret {
		secretName = c.Vdb.Spec.ClientServerTLSSecret
	} else {
		return secretName, fmt.Errorf("invalid secret: %d", secret)
	}
	return secretName, nil
}

func (c *TLSCertCache) SetSecretData(secretName string, data map[string][]byte) {
	c.certCacheMap[secretName] = data
}
