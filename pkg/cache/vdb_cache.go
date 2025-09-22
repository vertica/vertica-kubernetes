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

package cache

import (
	"context"
	"slices"
	"sync"
	"time"

	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

/*
    As operator code runs in multiple threads, it is not
	thread safe to define a variable at package level and
	share it. This file offers thread safe sharing/caching of
	certificate and other items, like password.

    Here is an example about how to get a reference to the cert cache
 	certCache := cache.GetCertCacheForVdb(vdb.Namespace, vdb.Name)

*/

type dbToCacheMap map[types.NamespacedName]*VdbCacheStruct

type CacheManagerStruct struct {
	allCacheMap  dbToCacheMap // map each vdb to a VdbContext
	guardAllLock *sync.Mutex  // guards allContextMap
	enabled      bool
}

type CacheManager interface {
	InitCacheForVdb(*v1.VerticaDB, *cloud.SecretFetcher)
	GetCertCacheForVdb(string, string) CertCache
	DestroyCacheForVdb(string, string)
	SetPassword(namespace, name, password string)
	GetPassword(namespace, name string) (string, bool)
	DeletePassword(namespace, name string)
}

// These are the functions that can set/read a bool/secert
type CertCache interface {
	ReadCertFromSecret(context.Context, string) (*tls.HTTPSCerts, error)
	ClearCacheBySecretName(string)
	IsCertInCache(string) bool
	CleanCacheForVdb([]string)
}

type ItemCache[T any] struct {
	lock    sync.Mutex
	items   map[string]itemWithTime[T]
	ttl     time.Duration
	enabled bool
}

type itemWithTime[T any] struct {
	value        T
	creationTime time.Time
}

func NewItemCache[T any](ttl time.Duration, enabled bool) *ItemCache[T] {
	return &ItemCache[T]{
		items:   make(map[string]itemWithTime[T]),
		ttl:     ttl,
		enabled: enabled,
	}
}

func (c *ItemCache[T]) Get(key string) (T, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.enabled {
		var zero T
		return zero, false
	}
	entry, ok := c.items[key]
	if !ok {
		var zero T
		return zero, false
	}
	if c.ttl > 0 && time.Since(entry.creationTime) > c.ttl {
		// Item expired, remove from cache
		delete(c.items, key)
		var zero T
		return zero, false
	}
	return entry.value, true
}

func (c *ItemCache[T]) Set(key string, value T) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.enabled {
		return
	}
	c.items[key] = itemWithTime[T]{
		value:        value,
		creationTime: time.Now(),
	}
}

func (c *ItemCache[T]) Delete(key string) {
	c.lock.Lock()
	defer c.lock.Unlock()
	if !c.enabled {
		return
	}
	delete(c.items, key)
}

type VdbCacheStruct struct {
	namespace    string // save namespace so it is not required to be passed
	fetcher      *cloud.SecretFetcher
	enabled      bool
	certCache    *ItemCache[map[string][]byte]
	genericCache *ItemCache[string] // Generic cache for single-value items like password
}

var log = ctrl.Log.WithName("vdb_cache")

const GenericPasswordKey = "password"

func MakeCacheManager(enabled bool) CacheManager {
	c := &CacheManagerStruct{}
	c.guardAllLock = &sync.Mutex{}
	c.allCacheMap = make(dbToCacheMap)
	c.enabled = enabled
	log.Info("initialized cache manager")
	return c
}

// InitCacheForVdb will initialize a Cache from a vdb
func (c *CacheManagerStruct) InitCacheForVdb(vdb *v1.VerticaDB, fetcher *cloud.SecretFetcher) {
	c.guardAllLock.Lock()
	defer c.guardAllLock.Unlock()
	vdbName := types.NamespacedName{
		Name:      vdb.Name,
		Namespace: vdb.Namespace,
	}
	vdbCache, ok := c.allCacheMap[vdbName]
	tlsCacheDuration := meta.GetCacheDuration(vdb.Annotations)
	if !ok {
		vdbCache = makeVdbCache(vdbName.Namespace, tlsCacheDuration, fetcher, c.enabled)
		c.allCacheMap[vdbName] = vdbCache
		log.Info("initialized cert cache for vdb", "vdbname", vdbName.Namespace, "vdbnamespace", vdbName.Name, "enabled", c.enabled)
	} else if vdbCache.certCache.ttl != time.Duration(tlsCacheDuration)*time.Second {
		vdbCache.certCache.ttl = time.Duration(tlsCacheDuration) * time.Second
		log.Info("cache expire duration has been updated", "new duration", vdb.GetCacheDuration())
	}
}

// GetCertCacheForVdb will return a CertCache from a vdb's name and namespace
func (c *CacheManagerStruct) GetCertCacheForVdb(namespace, name string) CertCache {
	c.guardAllLock.Lock()
	defer c.guardAllLock.Unlock()
	vdbName := types.NamespacedName{
		Name:      name,
		Namespace: namespace,
	}
	singleCertCache := c.allCacheMap[vdbName]
	return singleCertCache
}

// DestroyCacheForVdb will remove the cache for a vdb
// This is used when the vdb is deleted and we want to free up memory
func (c *CacheManagerStruct) DestroyCacheForVdb(namespace, name string) {
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

// makeVdbCache instantiates a VdbCacheStruct and saves
// vdb's namespace in it for convenience
func makeVdbCache(namespace string, ttl int, fetcher *cloud.SecretFetcher, enabled bool) *VdbCacheStruct {
	singleContext := &VdbCacheStruct{}
	singleContext.namespace = namespace
	singleContext.fetcher = fetcher
	singleContext.enabled = enabled
	singleContext.certCache = NewItemCache[map[string][]byte](time.Duration(ttl)*time.Second, enabled)
	singleContext.genericCache = NewItemCache[string](time.Duration(ttl)*time.Second, enabled)
	return singleContext
}

// ReadCertFromSecret will first try to load certs from its cache by secretName.
// If the secret is not found in cache, it will be loaded from k8s and be cached.
// the cache key will be the secretName
func (c *VdbCacheStruct) ReadCertFromSecret(ctx context.Context, secretName string) (*tls.HTTPSCerts, error) {
	var secretMap map[string][]byte
	readRequired := true

	if c.enabled {
		var ok bool
		secretMap, ok = c.certCache.Get(secretName)
		if ok {
			readRequired = false
		}
	}

	if readRequired {
		var err error
		secretMap, err = retrieveSecretByName(ctx, c.namespace, secretName, c.fetcher)
		if err != nil {
			return nil, err // failed to load secret
		}
		if c.enabled {
			c.certCache.Set(secretName, secretMap) // add secret content to cache
			log.Info("loaded tls secret and cached it", "secretName", secretName)
		} else {
			log.Info("loaded tls secret", "secretName", secretName)
		}
	}

	return &tls.HTTPSCerts{
		Key:    string(secretMap[corev1.TLSPrivateKeyKey]),
		Cert:   string(secretMap[corev1.TLSCertKey]),
		CaCert: string(secretMap[paths.HTTPServerCACrtName]),
	}, nil
}

// ClearCacheBySecretName will remove the cert referenced by secretName from cache
func (c *VdbCacheStruct) ClearCacheBySecretName(name string) {
	c.certCache.Delete(name)
}

func (c *VdbCacheStruct) SaveCertIntoCache(secretName string, certData map[string][]byte) {
	c.certCache.Set(secretName, certData)
}

// CleanCacheForVdb will delete secrets that are not used in spec or status
func (c *VdbCacheStruct) CleanCacheForVdb(secretsInUse []string) {
	c.certCache.lock.Lock()
	defer c.certCache.lock.Unlock()
	for key := range c.certCache.items {
		if !slices.Contains(secretsInUse, key) {
			delete(c.certCache.items, key)
		}
	}
}

// IsCertInCache checks whether a secret is stored in cache
func (c *VdbCacheStruct) IsCertInCache(secretName string) bool {
	c.certCache.lock.Lock()
	defer c.certCache.lock.Unlock()
	_, ok := c.certCache.items[secretName]
	return ok
}

// retrieveSecretByName read secret by secret name
func retrieveSecretByName(ctx context.Context, namespace, secretName string, fetcher *cloud.SecretFetcher) (map[string][]byte, error) {
	fetchName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}

func (c *CacheManagerStruct) SetPassword(namespace, name, password string) {
	vdbCache := c.GetCertCacheForVdb(namespace, name).(*VdbCacheStruct)
	vdbCache.genericCache.Set(GenericPasswordKey, password)
}

func (c *CacheManagerStruct) GetPassword(namespace, name string) (string, bool) {
	vdbCache := c.GetCertCacheForVdb(namespace, name).(*VdbCacheStruct)
	return vdbCache.genericCache.Get(GenericPasswordKey)
}

func (c *CacheManagerStruct) DeletePassword(namespace, name string) {
	vdbCache := c.GetCertCacheForVdb(namespace, name).(*VdbCacheStruct)
	vdbCache.genericCache.Delete(GenericPasswordKey)
}
