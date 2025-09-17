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

package vk8s

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getPasswordFromSecret retrieves the password from the secret using the provided key
func getPasswordFromSecret(secret map[string][]byte, key string) (*string, error) {
	var passwd string
	pwd, ok := secret[key]
	if !ok {
		return nil, fmt.Errorf("password not found, secret must have a key with name %q", key)
	}
	passwd = string(pwd)
	return &passwd, nil
}

// GetSuperuserPassword returns the superuser password if it has been provided
func GetSuperuserPassword(ctx context.Context, cl client.Client, log logr.Logger,
	e events.EVWriter, vdb *vapi.VerticaDB, cacheManager cache.CacheManager) (*string, error) {
	return GetCustomSuperuserPassword(ctx, cl, log, e, vdb,
		vdb.GetPasswordSecret(), names.SuperuserPasswordKey, cacheManager, false)
}

// GetCustomSuperuserPassword returns the superuser password stored in a custom secret.
// It first checks the Password cache; if not found, it fetches from K8s.
func GetCustomSuperuserPassword(ctx context.Context, cl client.Client, log logr.Logger,
	e events.EVWriter, vdb *vapi.VerticaDB,
	customPasswordSecret, customPasswordSecretKey string,
	cacheManager cache.CacheManager, skipCache bool) (*string, error) {
	// Check the cache first
	fetcher := &cloud.SecretFetcher{
		Client:   cl,
		Log:      log,
		Obj:      vdb,
		EVWriter: e,
	}
	cacheManager.InitCacheForVdb(vdb, fetcher)
	if cachedPw, ok := cacheManager.GetPassword(vdb.Namespace, vdb.Name); !skipCache && ok {
		return &cachedPw, nil
	}

	// Handle empty secret case
	if customPasswordSecret == "" {
		emptyPassword := ""
		return &emptyPassword, nil
	}

	secret, err := fetcher.Fetch(ctx, names.GenNamespacedName(vdb, customPasswordSecret))
	if err != nil {
		return nil, err
	}

	passwd, err := getPasswordFromSecret(secret, customPasswordSecretKey)
	if err != nil {
		return nil, err
	}

	// Cache the fetched password
	cacheManager.SetPassword(vdb.Namespace, vdb.Name, *passwd)
	return passwd, nil
}
