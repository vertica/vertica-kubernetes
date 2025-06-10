package vadmin

import (
	"context"
	"fmt"
	"sync"

	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

/*
    As operator code runs in multiple threads, it is not
	thread safe to a define a variable at package level and
	share it. This file offers thread safe sharing/caching of
	predefined variables.

    Here is an example about how to get a reference to the cache
 	vdbContext := vadmin.GetContextForVdb(c.Vdb.Namespace, c.Vdb.Name)

*/

const (
	// bool names
	UseTLSCert = "UseTlsCert"

	// secret names
	NMATLSSecret = "NMATLSSecret"
)

// These are the functions that can set/read a bool/secert
type VdbContext interface {
	LoadCertBySecretName(context.Context, string, cloud.SecretFetcher) (*HTTPSCerts, error)
	ClearCertCache(string)
	BackupCertCache(string, string) error
	LoadCertFromCache(string) (*HTTPSCerts, error)
	SaveCertIntoCache(string, map[string][]byte)
}

type VdbContextStruct struct {
	namespace     string      // save namespace so it is not required to be passed
	lockForSecret *sync.Mutex // lock that guards secrets
	secretMap     map[string]map[string][]byte
	// this pointer is used purely for ease of test purpose
	retrieveSecret func(context.Context, string, string, cloud.SecretFetcher) (map[string][]byte, error)
}

type contextMap map[types.NamespacedName]*VdbContextStruct

var lock = &sync.Mutex{} // guards allContextMap

// map each vdb to a VdbContext
var allContextMap contextMap

// GetContextForVdb will return a VdbContext from a vdb's name and namespace
func GetContextForVdb(namespace, name string) VdbContext {
	lock.Lock()
	defer lock.Unlock()
	vdbName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	if allContextMap == nil {
		allContextMap = make(contextMap)
	}
	singleContext, ok := allContextMap[vdbName]
	if !ok {
		singleContext = makeVdbContext(vdbName.Namespace)
		allContextMap[vdbName] = singleContext
	}
	return singleContext
}

// makeVdbContext instantiates a VdbContextStruct and saves
// vdb's namespace in it for convenience
func makeVdbContext(namespace string) *VdbContextStruct {
	singleContext := &VdbContextStruct{}
	singleContext.namespace = namespace
	singleContext.lockForSecret = &sync.Mutex{}
	singleContext.secretMap = make(map[string]map[string][]byte)
	singleContext.retrieveSecret = retrieveSecretByName
	return singleContext
}

// GetCertFromSecret will first try to load certs from its cache by secretName.
// If the secret is not found in cache, it will be loaded from k8s and be cached.
// the cache key will be the secretName
func (c *VdbContextStruct) LoadCertBySecretName(ctx context.Context, secretName string, fetcher cloud.SecretFetcher) (*HTTPSCerts, error) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	secretMap, ok := c.secretMap[secretName]
	err := error(nil)
	if !ok {
		secretMap, err = c.retrieveSecret(ctx, c.namespace, secretName, fetcher)
		if err != nil {
			return nil, err // failed to load secret
		}
		c.secretMap[secretName] = secretMap // add secret content to cache
	}
	return &HTTPSCerts{
		Key:    string(secretMap[corev1.TLSPrivateKeyKey]),
		Cert:   string(secretMap[corev1.TLSCertKey]),
		CaCert: string(secretMap[paths.HTTPServerCACrtName]),
	}, nil
}

// InvalidateCertCacheBySecretName will remove the cert referenced by secretName from cache
func (c *VdbContextStruct) ClearCertCache(name string) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	_, ok := c.secretMap[name]
	if !ok {
		return
	}
	delete(c.secretMap, name)
	return
}

// BackupCertCache will create new cache entry for the entry referenced by curretnName
// Note that this will not clone the cert. After this call, both entries point to the same cert
func (c *VdbContextStruct) BackupCertCache(currentName, newName string) error {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	cert, ok := c.secretMap[currentName]
	if !ok {
		return fmt.Errorf("failed to find cert %s in cache to back up", currentName)
	}
	c.secretMap[newName] = cert
	return nil
}

// LoadCertFromCache will try to load cert from cache using the given name key.
func (c *VdbContextStruct) LoadCertFromCache(name string) (*HTTPSCerts, error) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	secretMap, ok := c.secretMap[name]
	if !ok {
		return nil, fmt.Errorf("failed to find cert %s in the cache", name)
	} else {
		return &HTTPSCerts{
			Key:    string(secretMap[corev1.TLSPrivateKeyKey]),
			Cert:   string(secretMap[corev1.TLSCertKey]),
			CaCert: string(secretMap[paths.HTTPServerCACrtName]),
		}, nil
	}
}

func (c *VdbContextStruct) SaveCertIntoCache(name string, certData map[string][]byte) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	c.secretMap[name] = certData
}

// retrieveSecretByName loads secret from k8s by secret name
func retrieveSecretByName(ctx context.Context, namespace, secretName string, fetcher cloud.SecretFetcher) (map[string][]byte, error) {
	fetchName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}
