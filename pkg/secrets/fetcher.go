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

package secrets

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/crc32"

	gsm "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MultiSourceSecretFetcher is secret reader that handles retrival from
// different sources such as Kubernetes secret store and Google Secrets Manager
// (GSM).
type MultiSourceSecretFetcher struct {
	client.Client
	Log Logger
}

// Logger is a very simple logging interface for this package. Callers can use
// logr.Logger if they choose, but I opted not include that package in here to
// keep the number of dependencies low.
type Logger interface {
	// Info will write an info message to a logger. It's given a string message
	// following by a pair of key/values as context for the message.
	Info(msg string, keysAndValues ...any)
}

// NotFoundError is an error returned when the secret isn't found
type NotFoundError struct {
	msg string
}

func (e *NotFoundError) Error() string {
	return e.msg
}

// Fetch reads the secret from a secret store. The contents of the secret is successful.
func (m *MultiSourceSecretFetcher) Fetch(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, error) {
	switch {
	case IsGSMSecret(secretName.Name):
		return m.readFromGSM(ctx, secretName.Name)
	case IsAWSSecretsManagerSecret(secretName.Name):
		return nil, fmt.Errorf("fetching secret %s from Amazon Secrets Manager is not implemented", secretName.Name)
	default:
		return m.readFromK8s(ctx, secretName)
	}
}

// readFromK8s reads the secret using the K8s Secret API.
func (m *MultiSourceSecretFetcher) readFromK8s(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, error) {
	tlsCerts := &corev1.Secret{}
	err := m.Client.Get(ctx, secretName, tlsCerts)
	if err != nil {
		return nil, fmt.Errorf("could not fetch k8s secret named %s: %w", secretName, err)
	}
	return tlsCerts.Data, nil
}

// ReadFromGSM will fetch a secret from Google Secret Manager (GSM)
func (m *MultiSourceSecretFetcher) readFromGSM(ctx context.Context, secName string) (map[string][]byte, error) {
	clnt, err := gsm.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create secretmanager client")
	}
	defer clnt.Close()

	req := &secretmanagerpb.AccessSecretVersionRequest{
		Name: RemovePathReference(secName),
	}
	m.Log.Info("Reading secret from GSM", "name", req.Name)

	result, err := clnt.AccessSecretVersion(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("could not fetch secret: %w", err)
	}

	crc32c := crc32.MakeTable(crc32.Castagnoli)
	checksum := int64(crc32.Checksum(result.Payload.Data, crc32c))
	if checksum != *result.Payload.DataCrc32C {
		return nil, fmt.Errorf("data corruption detected")
	}
	contents := make(map[string][]byte)
	err = json.Unmarshal(result.Payload.Data, &contents)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal the contents of the GSM secret '%s': %w", secName, err)
	}
	return contents, nil
}
