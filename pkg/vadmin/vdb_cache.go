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

type dbToCacheMap map[types.NamespacedName]*VdbCacheStruct

type CacheManangerStruct struct {
	allCacheMap  dbToCacheMap // map each vdb to a VdbContext
	guardAllLock *sync.Mutex  // guards allContextMap
}

type CacheManager interface {
	InitCertCacheForVdb(string, string, *cloud.SecretFetcher)
	GetCertCacheForVdb(string, string) CertCache
	DestroyCertCacheForVdb(string, string)
}

// These are the functions that can set/read a bool/secert
type CertCache interface {
	ReadCertFromSecret(context.Context, string) (*HTTPSCerts, error)
	ClearCacheBySecretName(string)
	SaveCertIntoCache(string, map[string][]byte)
}

type VdbCacheStruct struct {
	namespace     string      // save namespace so it is not required to be passed
	lockForSecret *sync.Mutex // lock that guards secretMap and all APIs
	secretMap     map[string]map[string][]byte
	fetcher       *cloud.SecretFetcher
	// this pointer is used purely for ease of test purpose
	retrieveSecret func(context.Context, string, string, *cloud.SecretFetcher) (map[string][]byte, error)
}

func MakeCacheManager() CacheManager {
	c := &CacheManangerStruct{}
	c.guardAllLock = &sync.Mutex{}
	c.allCacheMap = make(dbToCacheMap)
	vcLog.Info("initialized cache manager")
	return c
}

// InitCertCacheForVdb will return a CertCache from a vdb's name and namespace
func (c *CacheManangerStruct) InitCertCacheForVdb(namespace, name string, fetcher *cloud.SecretFetcher) {
	c.guardAllLock.Lock()
	defer c.guardAllLock.Unlock()
	vdbName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	_, ok := c.allCacheMap[vdbName]
	if !ok {
		singleCertCache := makeVdbCertCache(vdbName.Namespace, fetcher)
		c.allCacheMap[vdbName] = singleCertCache
		vcLog.Info(fmt.Sprintf("initialized cert cache for vdb %s/%s", vdbName.Namespace, vdbName.Name))
	}
}

// GetCertCacheForVdb will return a CertCache from a vdb's name and namespace
func (c *CacheManangerStruct) GetCertCacheForVdb(namespace, name string) CertCache {
	c.guardAllLock.Lock()
	defer c.guardAllLock.Unlock()
	vdbName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	singleCertCache := c.allCacheMap[vdbName]
	return singleCertCache
}

// DestroyCertCacheForVdb will remove the cache for a vdb
// This is used when the vdb is deleted and we want to free up memory
func (c *CacheManangerStruct) DestroyCertCacheForVdb(namespace, name string) {
	c.guardAllLock.Lock()
	defer c.guardAllLock.Unlock()
	vdbName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	if c.allCacheMap != nil {
		delete(c.allCacheMap, vdbName)
	}
}

// makeVdbCertCache instantiates a VdbCacheStruct and saves
// vdb's namespace in it for convenience
func makeVdbCertCache(namespace string, fetcher *cloud.SecretFetcher) *VdbCacheStruct {
	singleContext := &VdbCacheStruct{}
	singleContext.namespace = namespace
	singleContext.lockForSecret = &sync.Mutex{}
	singleContext.secretMap = make(map[string]map[string][]byte)
	singleContext.fetcher = fetcher
	singleContext.retrieveSecret = retrieveSecretByName
	return singleContext
}

// ReadCertFromSecret will first try to load certs from its cache by secretName.
// If the secret is not found in cache, it will be loaded from k8s and be cached.
// the cache key will be the secretName
func (c *VdbCacheStruct) ReadCertFromSecret(ctx context.Context, secretName string) (*HTTPSCerts, error) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	secretMap, ok := c.secretMap[secretName]
	err := error(nil)
	if !ok {
		secretMap, err = c.retrieveSecret(ctx, c.namespace, secretName, c.fetcher)
		if err != nil {
			return nil, err // failed to load secret
		}
		c.secretMap[secretName] = secretMap // add secret content to cache
		vcLog.Info(fmt.Sprintf("loaded tls secret %s and cached it", secretName))
	}
	return &HTTPSCerts{
		Key:    string(secretMap[corev1.TLSPrivateKeyKey]),
		Cert:   string(secretMap[corev1.TLSCertKey]),
		CaCert: string(secretMap[paths.HTTPServerCACrtName]),
	}, nil
}

// ClearCacheBySecretName will remove the cert referenced by secretName from cache
func (c *VdbCacheStruct) ClearCacheBySecretName(name string) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	_, ok := c.secretMap[name]
	if !ok {
		return
	}
	delete(c.secretMap, name)
}

func (c *VdbCacheStruct) SaveCertIntoCache(name string, certData map[string][]byte) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	c.secretMap[name] = certData
}

// retrieveSecretByName read secret by secret name
func retrieveSecretByName(ctx context.Context, namespace, secretName string, fetcher *cloud.SecretFetcher) (map[string][]byte, error) {
	fetchName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}
