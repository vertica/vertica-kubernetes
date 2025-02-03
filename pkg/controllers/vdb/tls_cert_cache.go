package vdb

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const ( // iota is reset to 0
	NMA_TLS_SECRET = iota
	HTTPS_TLS_SECRET
	CLIENT_TLS_SECRET
)

var CERT_FIELDS = map[string]bool{
	corev1.TLSPrivateKeyKey:   true,
	corev1.TLSCertKey:         true,
	paths.HTTPServerCACrtName: true,
}

type TLSCertCache struct {
	client.Client
	Scheme       *runtime.Scheme
	Log          logr.Logger
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	certCacheMap map[string]map[string][]byte
}

func MakeTLSCertCache(cli client.Client, scheme *runtime.Scheme, log logr.Logger,
	vdb *vapi.VerticaDB) *TLSCertCache {
	return &TLSCertCache{
		Client:       cli,
		Scheme:       scheme,
		Log:          log.WithName("StatusReconciler"),
		Vdb:          vdb,
		certCacheMap: map[string]map[string][]byte{},
	}
}

func (c *TLSCertCache) GetTLSCert(secret int, fieldName string) ([]byte, error) {
	_, ok := CERT_FIELDS[fieldName]
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
	// not found in cache. load from secretes
	secretMap, err = c.retrieveSecret(secret)
	if err != nil {
		return nil, err
	}
	for field := range CERT_FIELDS {
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
	if secret == NMA_TLS_SECRET {
		secretName = c.Vdb.Spec.NMATLSSecret
	} else if secret == HTTPS_TLS_SECRET {
		secretName = c.Vdb.Spec.HTTPSTLSSecret
	} else if secret == CLIENT_TLS_SECRET {
		secretName = c.Vdb.Spec.ClientTLSSecret
	} else {
		return secretName, fmt.Errorf("invalid secret: %d", secret)
	}
	return secretName, nil
}
