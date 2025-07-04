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

package cloud

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SecretFetcher is secret reader designed for the controllers that need to read a secret.
// It can handle retrival from different sources, such as Kubernetes
// secret store, Google Secrets Manager (GSM), etc.
type SecretFetcher struct {
	client.Client
	Log      logr.Logger
	EVWriter events.EVWriter
	Obj      runtime.Object
}

// Fetch reads the secret from a secret store. The contents of the secret is successful.
func (v *SecretFetcher) Fetch(ctx context.Context, secretName types.NamespacedName) (map[string][]byte, error) {
	secretData, res, err := v.FetchAllowRequeue(ctx, secretName)
	if res.Requeue && err == nil {
		return secretData, fmt.Errorf("secret fetch ended with requeue but is not allowed in code path, secret name - %s", secretName.Name)
	}
	return secretData, err
}

// FetchAllowRequeue reads the secret from a secret store. This API has the
// ability to requeue the reconcile iteration based on the error it finds.
func (v *SecretFetcher) FetchAllowRequeue(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, ctrl.Result, error) {
	sf := secrets.MultiSourceSecretFetcher{
		K8sClient: v,
		Log:       v.Log,
	}
	secretData, err := sf.Fetch(ctx, secretName)
	if err != nil {
		return v.handleFetchError(secretName, err)
	}
	return secretData, ctrl.Result{}, err
}

// GetSecret will allow us to fulfill the client interface in
// MultiSourceSecretFetcher. It is a wrapper to get a resource using the
// controller's k8s client.
func (v *SecretFetcher) GetSecret(ctx context.Context, name types.NamespacedName) (*corev1.Secret, error) {
	secret := corev1.Secret{}
	err := v.Client.Get(ctx, name, &secret)
	return &secret, err
}

// handleFetchError is called when there is an error fetching the secret. It
// will handle things like event logging and setting up the ctrl.Result.
func (v *SecretFetcher) handleFetchError(secretName types.NamespacedName, err error) (map[string][]byte, ctrl.Result, error) {
	nfe := &secrets.NotFoundError{}
	if ok := errors.As(err, &nfe); ok {
		v.EVWriter.Eventf(v.Obj, corev1.EventTypeWarning, events.ObjectNotFound,
			"Could not find the secret '%s'", secretName.Name)
		return nil, ctrl.Result{Requeue: true}, nil
	}
	v.Log.Error(err, fmt.Sprintf("secret %s cannot be fetched", secretName.Name))
	return nil, ctrl.Result{}, err
}
