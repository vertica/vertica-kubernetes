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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// VerticaDBSecretFetcher is secret reader designed for the verticadb
// controller. It can handle retrival from different sources, such as Kubernetes
// secret store, Google Secrets Manager (GSM), etc.
type VerticaDBSecretFetcher struct {
	client.Client
	Log      logr.Logger
	EVWriter events.EVWriter
	VDB      *vapi.VerticaDB
}

// Fetch reads the secret from a secret store. The contents of the secret is successful.
func (v *VerticaDBSecretFetcher) Fetch(ctx context.Context, secretName types.NamespacedName) (map[string][]byte, error) {
	secretData, res, err := v.FetchAllowRequeue(ctx, secretName)
	if res.Requeue && err == nil {
		return secretData, fmt.Errorf("secret fetch ended with requeue but is not allowed in code path")
	}
	return secretData, err
}

// FetchAllowRequeue reads the secret from a secret store. This API has the
// ability to requeue the reconcile iteration based on the error it finds.
func (v *VerticaDBSecretFetcher) FetchAllowRequeue(ctx context.Context, secretName types.NamespacedName) (
	map[string][]byte, ctrl.Result, error) {
	sf := secrets.MultiSourceSecretFetcher{
		Client: v.Client,
		Log:    v.Log,
	}
	secretData, err := sf.Fetch(ctx, secretName)
	if err != nil {
		return v.handleFetchError(secretName, err)
	}
	return secretData, ctrl.Result{}, err
}

// handleFetchError is called when there is an error fetching the secret. It
// will handle things like event logging and setting up the ctrl.Result.
func (v *VerticaDBSecretFetcher) handleFetchError(secretName types.NamespacedName, err error) (map[string][]byte, ctrl.Result, error) {
	nfe := &secrets.NotFoundError{}
	if ok := errors.As(err, &nfe); ok {
		v.EVWriter.Eventf(v.VDB, corev1.EventTypeWarning, events.ObjectNotFound,
			"Could not find the secret '%s'", secretName.Name)
		return nil, ctrl.Result{Requeue: true}, nil
	}
	return nil, ctrl.Result{}, err
}
