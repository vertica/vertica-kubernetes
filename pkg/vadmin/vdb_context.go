package vadmin

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

type VdbContextStruct struct {
	tlsCertLock *sync.Mutex
	boolMap     map[string]bool
}

type VdbContext interface {
	SetBoolValue(string, bool)
	GetBoolValue(string) bool
}

const (
	UseTLSCert = "UseTlsCert"
)

type contextMap map[types.NamespacedName]*VdbContextStruct

var lock = &sync.Mutex{}

var allContextMap contextMap

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
		singleContext = &VdbContextStruct{}
		singleContext.tlsCertLock = &sync.Mutex{}
		singleContext.boolMap = make(map[string]bool)
		allContextMap[vdbName] = singleContext
	}
	return singleContext
}

func (c *VdbContextStruct) SetBoolValue(fieldName string, value bool) {
	c.tlsCertLock.Lock()
	defer c.tlsCertLock.Unlock()
	c.boolMap[fieldName] = value
}

func (c *VdbContextStruct) GetBoolValue(fieldName string) bool {
	c.tlsCertLock.Lock()
	defer c.tlsCertLock.Unlock()
	return c.boolMap[fieldName]
}
