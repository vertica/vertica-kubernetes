package vadmin

import (
	"context"
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

    Here is an example about how to set a value for a bool variable:

 	vdbContext := vadmin.GetContextForVdb(c.Vdb.Namespace, c.Vdb.Name)
	vdbContext.SetBoolValue(vadmin.UseTLSCert, false)

	steps to add a bool variable and set it up

	1 Define a const string in the below const section
	2 Follow the above example to set it to true or flase

    You can add HasBoolValue(string) bool if it is required
*/

const (
	// bool names
	UseTLSCert = "UseTlsCert"

	// secret names
	NMATLSSecret = "NMATLSSecret"
)

// These are the functions that can set/read a bool/secert
type VdbContext interface {
	// This sets a bool value for a bool variable.
	SetBoolValue(string, bool)
	// This returns the value of a bool variable.
	GetBoolValue(string) bool

	// This will read certificates from secrets
	// Secrets will be cached after the initial loading
	GetCertFromSecret(string, cloud.VerticaDBSecretFetcher) (*HTTPSCerts, error)

	// This is for testing
	// setCertForSecret(string, *HTTPSCerts)
}

type VdbContextStruct struct {
	namespace     string      // save namespace so it is not required to be passed
	lockForBool   *sync.Mutex // lock that guards bool variables.
	boolMap       map[string]bool
	lockForSecret *sync.Mutex // lock that guards secrets
	secretMap     map[string]map[string][]byte
	// this pointer is used purely for ease of test purpose
	retrieveSecret func(string, string, cloud.VerticaDBSecretFetcher) (map[string][]byte, error)
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
	singleContext.lockForBool = &sync.Mutex{}
	singleContext.boolMap = make(map[string]bool)

	singleContext.lockForSecret = &sync.Mutex{}
	singleContext.secretMap = make(map[string]map[string][]byte)
	singleContext.retrieveSecret = retrieveSecretByName
	return singleContext
}

// SetBoolValue sets a bool value for a bool variable.
func (c *VdbContextStruct) SetBoolValue(fieldName string, value bool) {
	c.lockForBool.Lock()
	defer c.lockForBool.Unlock()
	c.boolMap[fieldName] = value
}

// This returns the value of a bool variable. Input is variable name
func (c *VdbContextStruct) GetBoolValue(fieldName string) bool {
	c.lockForBool.Lock()
	defer c.lockForBool.Unlock()
	return c.boolMap[fieldName]
}

// GetCertFromSecret will first try to get certs from its secretMap
// If the secret is not found in map, it will be loaded from k8s and be cached
func (c *VdbContextStruct) GetCertFromSecret(secretName string, fetcher cloud.VerticaDBSecretFetcher) (*HTTPSCerts, error) {
	c.lockForSecret.Lock()
	defer c.lockForSecret.Unlock()
	secretMap, ok := c.secretMap[secretName]
	err := error(nil)
	if !ok {
		secretMap, err = c.retrieveSecret(c.namespace, secretName, fetcher)
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

// retrieveSecretByName loads secret from k8s by secret name
func retrieveSecretByName(namespace, secretName string, fetcher cloud.VerticaDBSecretFetcher) (map[string][]byte, error) {
	ctx := context.Background()
	fetchName := types.NamespacedName{
		Namespace: namespace,
		Name:      secretName,
	}
	return fetcher.Fetch(ctx, fetchName)
}
