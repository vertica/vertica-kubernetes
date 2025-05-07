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

package vdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AddtionalBucketsReconciler will add additional buckets for data replication
type AddtionalBucketsReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts     *podfacts.PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeAddtionalBucketsReconciler will build a AddtionalBucketsReconciler object
func MakeAddtionalBucketsReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &AddtionalBucketsReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("AddtionalBucketsReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

func (a *AddtionalBucketsReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if a.Vdb.Spec.AdditionalBuckets != nil {
		return a.addAdditionalBuckets(ctx)
	}

	return ctrl.Result{}, errors.New("no addtional buckets set in spec")
}

// GetCredsSecret returns the contents of the credentials
// secret. It handles if the secret is not found and will log an event.
func (a *AddtionalBucketsReconciler) GetCredsSecret(ctx context.Context, credsSecret string) (map[string][]byte, ctrl.Result, error) {
	fetcher := cloud.SecretFetcher{
		Client:   a.VRec.GetClient(),
		Log:      a.Log,
		Obj:      a.Vdb,
		EVWriter: a.VRec,
	}
	return fetcher.FetchAllowRequeue(ctx, names.GenNamespacedName(a.Vdb, credsSecret))
}

// getAuth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (a *AddtionalBucketsReconciler) GetAuth(ctx context.Context, credsSecret string) (string, string, ctrl.Result, error) {
	secret, res, err := a.GetCredsSecret(ctx, credsSecret)
	if verrors.IsReconcileAborted(res, err) {
		return "", "", res, err
	}

	accessKey, ok := secret[cloud.CommunalAccessKeyName]
	if !ok {
		a.VRec.Eventf(a.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' does not have a key named '%s'", credsSecret, cloud.CommunalAccessKeyName)
		return "", "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret[cloud.CommunalSecretKeyName]
	if !ok {
		a.VRec.Eventf(a.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' does not have a key named '%s'", credsSecret, cloud.CommunalSecretKeyName)
		return "", "", ctrl.Result{Requeue: true}, nil
	}

	return string(accessKey), string(secretKey), ctrl.Result{}, nil
}

// getAzureAuth gets the azure credentials from the communal auth secret
func (a *AddtionalBucketsReconciler) GetAzureAuth(ctx context.Context, credsSecret string) (
	cloud.AzureCredential, cloud.AzureEndpointConfig, ctrl.Result, error) {
	secretData, res, err := a.GetCredsSecret(ctx, credsSecret)
	if verrors.IsReconcileAborted(res, err) {
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, res, err
	}

	accountName, hasAccountName := secretData[cloud.AzureAccountName]
	blobEndpointRaw, hasBlobEndpoint := secretData[cloud.AzureBlobEndpoint]

	if !hasAccountName && !hasBlobEndpoint {
		a.VRec.Eventf(a.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' is not setup properly for azure.  It must have one '%s' or '%s'",
			credsSecret, cloud.AzureAccountName, cloud.AzureBlobEndpoint)
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, ctrl.Result{Requeue: true}, nil
	}

	// The blob endpoint may have a protocol scheme as a prefix.  Strip that off
	// so its just the host and port.
	var blobEndpoint string
	if hasBlobEndpoint {
		blobEndpoint = config.GetEndpointHostPort(string(blobEndpointRaw))
	}

	accountKey, hasAccountKey := secretData[cloud.AzureAccountKey]
	sas, hasSAS := secretData[cloud.AzureSharedAccessSignature]

	if hasAccountKey && hasSAS {
		a.VRec.Eventf(a.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' is not setup properly for azure.  It cannot have both '%s' and '%s'",
			credsSecret, cloud.AzureAccountKey, cloud.AzureSharedAccessSignature)
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, ctrl.Result{Requeue: true}, nil
	}

	return cloud.AzureCredential{
			AccountName:           string(accountName),
			BlobEndpoint:          blobEndpoint,
			AccountKey:            string(accountKey),
			SharedAccessSignature: string(sas),
		},
		cloud.AzureEndpointConfig{
			AccountName:  string(accountName),
			BlobEndpoint: blobEndpoint,
			Protocol:     config.GetEndpointProtocol(string(blobEndpointRaw)),
		},
		ctrl.Result{}, nil
}

func (a *AddtionalBucketsReconciler) addAdditionalBuckets(ctx context.Context) (ctrl.Result, error) {
	var res ctrl.Result
	var err error

	sb := strings.Builder{}

	for _, bucket := range a.Vdb.Spec.AdditionalBuckets {
		// using s3
		if strings.HasPrefix(bucket.Path, v1.S3Prefix) {
			accessKey, secretKey, res, err := a.GetAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET S3BucketConfig = '[{\"bucket\": \"%s\", \"region\": \"%s\", \"protocol\": \"%s\", \"endpoint\": \"%s\"}]';`,
				config.GetBucket(bucket.Path), bucket.Region, config.GetEndpointProtocol(bucket.Endpoint), config.GetEndpoint(bucket.Endpoint)))

			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET S3BucketCredentials = '[{\"bucket\": \"%s\", \"accessKey\": \"%s\", \"secretAccessKey\": \"%s\"}]';`,
				config.GetBucket(bucket.Path), accessKey, secretKey))
		}

		// using gs
		if strings.HasPrefix(bucket.Path, v1.GCloudPrefix) {
			accessKey, secretKey, res, err := a.GetAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			sb.WriteString(fmt.Sprintf(
				`ALTER SESSION SET GCSAuth='%s:%s';`, accessKey, secretKey))
		}

		// using azb
		if strings.HasPrefix(bucket.Path, v1.AzurePrefix) {
			azureCreds, azureConfig, res, err := a.GetAzureAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET AzureStorageCredentials = '[{\"accountName\": \"%s\", \"accountKey\": \"%s\"}]';`,
				azureCreds.AccountName, azureCreds.AccountKey))
			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET AzureStorageEndpointConfig = '[{\"accountName\": \"%s\", \"blobEndpoint\": \"%s\", \"protocol\":\"%s\"}]';`,
				azureCreds.AccountName, azureConfig.BlobEndpoint, azureConfig.Protocol))
		}
	}

	return res, err
}
