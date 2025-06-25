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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AddtionalBucketsReconciler will add additional buckets for data replication
type AddtionalBucketsReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts  *podfacts.PodFacts
	PRunner cmds.PodRunner
}

// MakeAddtionalBucketsReconciler will build an AddtionalBucketsReconciler object
func MakeAddtionalBucketsReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &AddtionalBucketsReconciler{
		VRec:    vdbrecon,
		Log:     log.WithName("AddtionalBucketsReconciler"),
		Vdb:     vdb,
		PFacts:  pfacts,
		PRunner: prunner,
	}
}

func (a *AddtionalBucketsReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if a.Vdb.Spec.AdditionalBuckets == nil {
		return ctrl.Result{}, nil
	}

	// We put the same content to the status if additional buckets are added to the DB
	// No actions needed if status content is the same to spec
	if a.statusMatchesSpec() {
		return ctrl.Result{}, nil
	}

	res, err := a.updateAdditionalBuckets(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, a.updateAdditionalBucketsStatus(ctx)
}

// statusMatchesSpec checks if the additional buckets status is the same to spec or not
func (a *AddtionalBucketsReconciler) statusMatchesSpec() bool {
	if a.Vdb.Status.AdditionalBuckets == nil {
		return false
	}

	// status should have the same length as spec
	if len(a.Vdb.Spec.AdditionalBuckets) != len(a.Vdb.Status.AdditionalBuckets) {
		return false
	}

	// check if the additional buckets need to be updated
	for i, bucket := range a.Vdb.Spec.AdditionalBuckets {
		if a.Vdb.Status.AdditionalBuckets[i].Path != bucket.Path {
			return false
		}
		if a.Vdb.Status.AdditionalBuckets[i].Region != bucket.Region {
			return false
		}
		if a.Vdb.Status.AdditionalBuckets[i].Endpoint != bucket.Endpoint {
			return false
		}
		if a.Vdb.Status.AdditionalBuckets[i].CredentialSecret != bucket.CredentialSecret {
			return false
		}
	}

	return true
}

// updateAdditionalBucketsStatus will update additional buckets status in vdb
func (a *AddtionalBucketsReconciler) updateAdditionalBucketsStatus(ctx context.Context) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		if a.Vdb.Spec.AdditionalBuckets == nil {
			return nil
		}

		// simply make a copy of the additional buckets in the status
		vdbChg.Status.AdditionalBuckets = a.Vdb.Spec.AdditionalBuckets
		return nil
	}

	return vdbstatus.Update(ctx, a.VRec.GetClient(), a.Vdb, updateStatus)
}

// updateAdditionalBuckets will update the additional buckets in the database
func (a *AddtionalBucketsReconciler) updateAdditionalBuckets(ctx context.Context) (ctrl.Result, error) {
	var res ctrl.Result
	var err error
	var accessKey, secretKey string

	pf, found := a.PFacts.FindFirstUpPod(true, a.Vdb.GetFirstPrimarySubcluster().Name)
	if !found {
		return ctrl.Result{Requeue: true}, nil
	}

	var s3BucketConfigs []string
	var s3BucketCreds []string
	sb := strings.Builder{}

	for _, bucket := range a.Vdb.Spec.AdditionalBuckets {
		// using s3
		if strings.HasPrefix(bucket.Path, vapi.S3Prefix) {
			accessKey, secretKey, res, err = a.GetAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			s3BucketConfigs = append(s3BucketConfigs, fmt.Sprintf(
				`{"bucket": %q, "region": %q, "protocol": %q, "endpoint": %q}`,
				vapi.GetBucket(bucket.Path), bucket.Region, config.GetEndpointProtocol(bucket.Endpoint),
				config.GetEndpoint(bucket.Endpoint)))

			s3BucketCreds = append(s3BucketCreds, fmt.Sprintf(
				`{"bucket": %q, "accessKey": %q, "secretAccessKey": %q}`,
				vapi.GetBucket(bucket.Path), accessKey, secretKey))
		}

		// using gs
		if strings.HasPrefix(bucket.Path, vapi.GCloudPrefix) {
			if strings.HasPrefix(a.Vdb.Spec.Communal.Path, vapi.GCloudPrefix) {
				a.VRec.Log.Error(err, "cannot overwrite existing GCS parameters",
					"additional bucket path", bucket.Path, "existing gs path", a.Vdb.Spec.Communal.Path)
				return ctrl.Result{}, err
			}

			accessKey, secretKey, res, err = a.GetAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET GCSAuth='%s:%s';`, accessKey, secretKey))
		}

		// using azb
		if strings.HasPrefix(bucket.Path, vapi.AzurePrefix) {
			if strings.HasPrefix(a.Vdb.Spec.Communal.Path, vapi.AzurePrefix) {
				a.VRec.Log.Error(err, "cannot overwrite existing Azure parameters",
					"additional bucket path", bucket.Path, "existing azb path", a.Vdb.Spec.Communal.Path)
				return ctrl.Result{}, err
			}

			var azureCreds cloud.AzureCredential
			var azureConfig cloud.AzureEndpointConfig
			azureCreds, azureConfig, res, err = a.GetAzureAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET AzureStorageCredentials = '[{"accountName": %q, "accountKey": %q}]';`,
				azureCreds.AccountName, azureCreds.AccountKey))
			sb.WriteString(fmt.Sprintf(
				`ALTER DATABASE default SET AzureStorageEndpointConfig = '[{"accountName": %q, "blobEndpoint": %q, "protocol":%q}]';`,
				azureCreds.AccountName, azureConfig.BlobEndpoint, azureConfig.Protocol))
		}
	}

	// Write S3 configs if any S3 buckets were found
	if len(s3BucketConfigs) > 0 {
		sb.WriteString(fmt.Sprintf(
			`ALTER DATABASE default SET S3BucketConfig = '[%s]';`, strings.Join(s3BucketConfigs, ","),
		))
	}
	if len(s3BucketCreds) > 0 {
		sb.WriteString(fmt.Sprintf(
			`ALTER DATABASE default SET S3BucketCredentials = '[%s]';`, strings.Join(s3BucketCreds, ","),
		))
	}

	cmd := []string{"-tAc", sb.String()}
	stdout, stderr, err := a.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		a.VRec.Log.Error(err, "failed to retrieve active sessions", "stderr", stderr)
		return ctrl.Result{}, err
	}

	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.AdditionalBucketsUpdated,
		"Additional buckets updated")
	a.VRec.Log.Info("Updating additional buckets", "stdout", stdout)

	return res, err
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
func (a *AddtionalBucketsReconciler) GetAuth(ctx context.Context, credsSecret string) (accesskey, secretkey string,
	res ctrl.Result, err error) {
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
