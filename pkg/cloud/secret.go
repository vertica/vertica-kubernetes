/*
 (c) Copyright [2021-2023] Open Text.
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

package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"

	gsm "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MultiSourceSecretFetcher is secret reader that handles retrival from
// different sources such as Kubernetes secret store and Google Secrets Manager
// (GSM).
type MultiSourceSecretFetcher struct {
	client.Client
	Log      logr.Logger
	EVWriter events.EVWriter
	VDB      *vapi.VerticaDB
}

func (m *MultiSourceSecretFetcher) Fetch(ctx context.Context, secretName types.NamespacedName) (map[string][]byte, error) {
	secretData, res, err := m.FetchAllowRequeue(ctx, secretName)
	if res.Requeue && err == nil {
		return secretData, fmt.Errorf("secret fetch ended with requeue but is not allowed in code path")
	}
	return secretData, err
}

func (m *MultiSourceSecretFetcher) FetchAllowRequeue(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, ctrl.Result, error) {
	switch {
	case IsGSMSecret(secretName.Name):
		return m.readFromGSM(ctx, secretName.Name)
	default:
		return m.readFromK8s(ctx, secretName)
	}
}

func (m *MultiSourceSecretFetcher) readFromK8s(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, ctrl.Result, error) {
	tlsCerts := &corev1.Secret{}
	err := m.Client.Get(ctx, secretName, tlsCerts)
	if err != nil {
		if errors.IsNotFound(err) {
			m.EVWriter.Eventf(m.VDB, corev1.EventTypeWarning, events.ObjectNotFound,
				"Could not find the secret '%s'", secretName.Name)
			return nil, ctrl.Result{Requeue: true}, nil
		}
		return nil, ctrl.Result{}, fmt.Errorf("could not fetch k8s secret named %s: %w", secretName, err)
	}
	return tlsCerts.Data, ctrl.Result{}, nil
}

// ReadFromGSM will fetch a secret from Google Secret Manager (GSM)
func (m *MultiSourceSecretFetcher) readFromGSM(ctx context.Context, secName string) (map[string][]byte, ctrl.Result, error) {
	clnt, err := gsm.NewClient(ctx)
	if err != nil {
		return nil, ctrl.Result{}, fmt.Errorf("failed to create secretmanager client")
	}
	defer clnt.Close()

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: RemovePathReference(secName),
	}
	m.Log.Info("Reading secret from GSM", "name", req.Name)

	result, err := clnt.AccessSecretVersion(ctx, req)
	if err != nil {
		return nil, ctrl.Result{}, fmt.Errorf("could not fetch secret: %w", err)
	}

	crc32c := crc32.MakeTable(crc32.Castagnoli)
	checksum := int64(crc32.Checksum(result.Payload.Data, crc32c))
	if checksum != *result.Payload.DataCrc32C {
		return nil, ctrl.Result{}, fmt.Errorf("data corruption detected")
	}
	contents := make(map[string][]byte)
	err = json.Unmarshal(result.Payload.Data, &contents)
	if err != nil {
		return nil, ctrl.Result{}, fmt.Errorf("failed to unmarshal the contents of the GSM secret '%s': %w", secName, err)
	}
	return contents, ctrl.Result{}, nil
}
