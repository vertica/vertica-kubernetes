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

package security

import (
	"sync"

	"k8s.io/apimachinery/pkg/types"
)

// PasswordManager is the interface for storing and retrieving
// DB admin passwords across VDBs and sandboxes.
type PasswordManager interface {
	Set(nsName types.NamespacedName, pw string)
	Get(nsName types.NamespacedName) (string, bool)
	Delete(nsName types.NamespacedName)
}

// passwordManager is the concrete implementation of PasswordManager.
type passwordManager struct {
	mu        sync.RWMutex
	passwords map[types.NamespacedName]string
}

// NewPasswordManager creates a new PasswordManager.
func NewPasswordManager() PasswordManager {
	return &passwordManager{
		passwords: make(map[types.NamespacedName]string),
	}
}

func (pm *passwordManager) Set(nsName types.NamespacedName, pw string) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.passwords[nsName] = pw
}

func (pm *passwordManager) Get(nsName types.NamespacedName) (string, bool) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	pw, ok := pm.passwords[nsName]
	return pw, ok
}

func (pm *passwordManager) Delete(nsName types.NamespacedName) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	delete(pm.passwords, nsName)
}
