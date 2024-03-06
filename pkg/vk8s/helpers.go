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
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetSuperuserPassword returns the superuser password if it has been provided
func GetSuperuserPassword(ctx context.Context, cl client.Client, log logr.Logger,
	e events.EVWriter, vdb *vapi.VerticaDB) (string, error) {
	if vdb.Spec.PasswordSecret == "" {
		return "", nil
	}

	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   cl,
		Log:      log,
		VDB:      vdb,
		EVWriter: e,
	}
	secret, err := fetcher.Fetch(ctx, names.GenSUPasswdSecretName(vdb))
	if err != nil {
		return "", err
	}

	pwd, ok := secret[names.SuperuserPasswordKey]
	if !ok {
		return "", fmt.Errorf("password not found, secret must have a key with name '%s'", names.SuperuserPasswordKey)
	}
	return string(pwd), nil
}
