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

package vdb

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	AWSRegionParm          = "awsregion"
	GCloudRegionParm       = "GCSRegion"
	S3SseCustomerAlgorithm = "S3SseCustomerAlgorithm"
	S3ServerSideEncryption = "S3ServerSideEncryption"
	S3SseCustomerKey       = "S3SseCustomerKey"
	SseAlgorithmAES256     = "AES256"
	SseAlgorithmAWSKMS     = "aws:kms"
)

type DatabaseInitializer interface {
	getPodList() ([]*PodFact, bool)
	findPodToRunInit() (*PodFact, bool)
	execCmd(ctx context.Context, initiatorPod types.NamespacedName, hostList []string) (ctrl.Result, error)
	preCmdSetup(ctx context.Context, initiatorPod types.NamespacedName, podList []*PodFact) (ctrl.Result, error)
	postCmdCleanup(ctx context.Context) (ctrl.Result, error)
}

type GenericDatabaseInitializer struct {
	initializer         DatabaseInitializer
	VRec                *VerticaDBReconciler
	Log                 logr.Logger
	Vdb                 *vapi.VerticaDB
	PRunner             cmds.PodRunner
	PFacts              *PodFacts
	ConfigurationParams map[string]string
}

// checkAndRunInit will check if the database needs to be initialized and run init if applicable
func (g *GenericDatabaseInitializer) checkAndRunInit(ctx context.Context) (ctrl.Result, error) {
	if err := g.PFacts.Collect(ctx, g.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if !g.PFacts.doesDBExist() {
		res, err := g.runInit(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// runInit will physically setup the database.
// Depending on g.initializer, this will either do create_db or revive_db.
func (g *GenericDatabaseInitializer) runInit(ctx context.Context) (ctrl.Result, error) {
	podList, ok := g.initializer.getPodList()
	if !ok {
		// Was not able to generate the pod list
		return ctrl.Result{Requeue: true}, nil
	}
	ok = g.checkPodList(podList)
	if !ok {
		g.Log.Info("Aborting reconciliation as not all required pods are running")
		return ctrl.Result{Requeue: true}, nil
	}

	initPodFact, ok := g.initializer.findPodToRunInit()
	if !ok {
		// Could not find a runable pod to run from.
		return ctrl.Result{Requeue: true}, nil
	}
	initiatorPod := initPodFact.name

	res, err := g.ConstructAuthParms(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if res, err := g.initializer.preCmdSetup(ctx, initiatorPod, podList); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Cleanup for any prior failed attempt.
	if err := g.prepLocalDataInPods(ctx, podList); err != nil {
		return ctrl.Result{}, err
	}

	if res, err := g.initializer.execCmd(ctx, initiatorPod, getHostList(podList)); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	cond := vapi.VerticaDBCondition{Type: vapi.DBInitialized, Status: corev1.ConditionTrue}
	if err := vdbstatus.UpdateCondition(ctx, g.VRec.Client, g.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}

	// The DB has been initialized. We invalidate the cache now so that next
	// access will refresh with the new db state. A status reconciler will
	// follow this that will update the Vdb status about the db existence.
	g.PFacts.Invalidate()

	// Handle any post initialization actions
	return g.initializer.postCmdCleanup(ctx)
}

// checkPodList ensures all of the pods that we will use for the init call are running
func (g *GenericDatabaseInitializer) checkPodList(podList []*PodFact) bool {
	for _, pod := range podList {
		// Bail if find one of the pods isn't running or doesn't have the
		// annotations that we use in the k8s Vertica DC table.
		if !pod.isPodRunning || !pod.hasDCTableAnnotations {
			return false
		}
	}
	return true
}

// prepLocalDataInPods will go through each pod and ensure their local files are
// prepared correctly.  This step is necessary because a failed create_db can
// leave old state around.
func (g *GenericDatabaseInitializer) prepLocalDataInPods(ctx context.Context, podList []*PodFact) error {
	for _, pod := range podList {
		// Cleanup any local paths. This step is needed if an earlier create_db
		// fails -- admintools does not clean everything up.
		if err := prepLocalData(ctx, g.Vdb, g.PRunner, pod.name); err != nil {
			return err
		}
	}
	return nil
}

// ConstructAuthParms builds the authentication parms.
func (g *GenericDatabaseInitializer) ConstructAuthParms(ctx context.Context) (ctrl.Result, error) {
	var contentGen func(ctx context.Context) (ctrl.Result, error)

	if g.Vdb.Spec.Communal.Path == "" {
		g.Log.Info("Communal path is empty. Not setting up communal auth parms")
		return ctrl.Result{}, nil
	}

	if g.Vdb.IsS3() {
		contentGen = g.getS3AuthParmsContent
	} else if g.Vdb.IsHDFS() {
		contentGen = g.getHDFSAuthParmsContent
	} else if g.Vdb.IsGCloud() {
		contentGen = g.getGCloudAuthParmsContent
	} else if g.Vdb.IsAzure() {
		contentGen = g.getAzureAuthParmsContent
	} else {
		g.Log.Info("No special auth setup for communal path", "path", g.Vdb.Spec.Communal.Path)
	}

	var res ctrl.Result
	var err error
	if contentGen != nil {
		res, err = contentGen(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	if g.Vdb.HasKerberosConfig() {
		if res = g.hasCompatibleVersionForKerberos(); verrors.IsReconcileAborted(res, nil) {
			return res, nil
		}
		g.getKerberosAuthParmsContent()
	}

	// Add any additional config parameters that were included in the CR.
	// To avoid duplicate values, if a parameter is already set through another CR field,
	// (like S3ServerSideEncryption through communal.s3ServerSideEncryption), the corresponding
	// key/value pair in this map is skipped.
	// This must be the last thing added to the auth parms.
	if !g.Vdb.IsAdditionalConfigMapEmpty() {
		g.getAdditionalConfigParmsContent()
	}

	return ctrl.Result{}, nil
}

// getAuth will retrieve the auth parms if they exist.  If no credential secret
// then an empty string is returned.
func (g *GenericDatabaseInitializer) getAuth(ctx context.Context, parmName string) (ctrl.Result, error) {
	if g.Vdb.Spec.Communal.CredentialSecret == "" {
		return ctrl.Result{}, nil
	}

	// Extract the auth from the credential secret.
	auth, res, err := g.getCommunalAuth(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.ConfigurationParams[parmName] = auth
	return ctrl.Result{}, nil
}

// getS3AuthParmsContent adds auth parms to the config parms map when using S3
// communal storage.
func (g *GenericDatabaseInitializer) getS3AuthParmsContent(ctx context.Context) (ctrl.Result, error) {
	res, err := g.getAuth(ctx, "awsauth")
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	if g.Vdb.IsKnownSseType() {
		if res = g.hasCompatibleVersionForServerSideEncryption(); verrors.IsReconcileAborted(res, nil) {
			return res, nil
		}
		res, err = g.getServerSideEncryptionParmsContent(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	g.ConfigurationParams["awsendpoint"] = g.getCommunalEndpoint()
	g.ConfigurationParams["awsenablehttps"] = g.getEnableHTTPS()
	g.getRegion(AWSRegionParm)
	g.getCAFile()
	return ctrl.Result{}, nil
}

// getHDFSAuthParmsContent adds auth parms to the config parms map when using
// HDFS communal storage.
func (g *GenericDatabaseInitializer) getHDFSAuthParmsContent(ctx context.Context) (ctrl.Result, error) {
	g.getHadoopConfDir()
	g.getCAFile()
	return ctrl.Result{}, nil
}

// getKerberosAuthParmsContent adds Kerberos related auth parms to the config parms map.
// Must have Kerberos config in the Vdb.
func (g *GenericDatabaseInitializer) getKerberosAuthParmsContent() {
	// We disable KerberosEnableKeytabPermissionCheck, otherwise the engine will
	// complain that the keytab file doesn't have read/write permissions from
	// dbadmin only.
	g.ConfigurationParams["KerberosServiceName"] = g.Vdb.Spec.Communal.KerberosServiceName
	g.ConfigurationParams["KerberosRealm"] = g.Vdb.Spec.Communal.KerberosRealm
	g.ConfigurationParams["KerberosKeytabFile"] = paths.Krb5Keytab
	g.ConfigurationParams["KerberosEnableKeytabPermissionCheck"] = "0"
}

// getGCloudAuthParmsContent adds auth parms to the config parms map when we are
// connecting to google cloud storage.
func (g *GenericDatabaseInitializer) getGCloudAuthParmsContent(ctx context.Context) (ctrl.Result, error) {
	res, err := g.getAuth(ctx, "GCSAuth")
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.ConfigurationParams["GCSEndpoint"] = g.getCommunalEndpoint()
	g.ConfigurationParams["GCSEnableHttps"] = g.getEnableHTTPS()
	g.getRegion(GCloudRegionParm)
	g.getCAFile()
	return ctrl.Result{}, nil
}

// getAzureAuthParmsContent adds auth parms to the config parms map for an EON database created in
// Azure Blob Storage
func (g *GenericDatabaseInitializer) getAzureAuthParmsContent(ctx context.Context) (ctrl.Result, error) {
	if g.Vdb.Spec.Communal.CredentialSecret == "" {
		return ctrl.Result{}, nil
	}

	azureCreds, azureConfig, res, err := g.getAzureAuth(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	var azureCredsJSON strings.Builder
	elemPrefix := ""
	azureCredsJSON.WriteString("[{")
	if azureCreds.AccountName != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`"accountName": %q`, azureCreds.AccountName))
		elemPrefix = ","
	}
	if azureCreds.BlobEndpoint != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"blobEndpoint": %q`, elemPrefix, azureCreds.BlobEndpoint))
		elemPrefix = ","
	}
	if azureCreds.AccountKey != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"accountKey": %q`, elemPrefix, azureCreds.AccountKey))
		elemPrefix = ","
	}
	if azureCreds.SharedAccessSignature != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"sharedAccessSignature": %q`, elemPrefix, azureCreds.SharedAccessSignature))
	}
	azureCredsJSON.WriteString("}]")

	var azureConfigJSON strings.Builder
	elemPrefix = ""
	azureConfigJSON.WriteString("[{")
	if azureConfig.AccountName != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`"accountName": %q`, azureConfig.AccountName))
		elemPrefix = ","
	}
	if azureConfig.BlobEndpoint != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`%s"blobEndpoint": %q`, elemPrefix, azureConfig.BlobEndpoint))
		elemPrefix = ","
	}
	if azureConfig.Protocol != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`%s"protocol": %q`, elemPrefix, azureConfig.Protocol))
	}
	azureConfigJSON.WriteString("}]")

	g.ConfigurationParams["AzureStorageCredentials"] = azureCredsJSON.String()
	g.ConfigurationParams["AzureStorageEndpointConfig"] = azureConfigJSON.String()
	g.getCAFile()
	return ctrl.Result{}, nil
}

// getAdditionalConfigParmsContent adds additional server config parameters
// to the config parms map
func (g *GenericDatabaseInitializer) getAdditionalConfigParmsContent() {
	parmNames := g.genLowerCaseKeysMap()
	for k, v := range g.Vdb.Spec.Communal.AdditionalConfig {
		// we lowercase parm names to catch duplications
		// like AWSauth/awsauth. In case of duplicate names,
		// we skip to the next parm.
		_, ok := parmNames[strings.ToLower(k)]
		if ok {
			g.Log.Info("additional config parameter ignored", "parameter", k)
			continue
		}
		g.ConfigurationParams[k] = v
	}
}

// getCommunalAuth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (g *GenericDatabaseInitializer) getCommunalAuth(ctx context.Context) (string, ctrl.Result, error) {
	secret, res, err := g.getCommunalCredsSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return "", res, err
	}

	accessKey, ok := secret.Data[cloud.CommunalAccessKeyName]
	if !ok {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, cloud.CommunalAccessKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret.Data[cloud.CommunalSecretKeyName]
	if !ok {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, cloud.CommunalSecretKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	auth := fmt.Sprintf("%s:%s", strings.TrimSuffix(string(accessKey), "\n"),
		strings.TrimSuffix(string(secretKey), "\n"))
	return auth, ctrl.Result{}, nil
}

// getAzureAuth gets the azure credentials from the communal auth secret
func (g *GenericDatabaseInitializer) getAzureAuth(ctx context.Context) (
	cloud.AzureCredential, cloud.AzureEndpointConfig, ctrl.Result, error) {
	secret, res, err := g.getCommunalCredsSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, res, err
	}

	accountName, hasAccountName := secret.Data[cloud.AzureAccountName]
	blobEndpointRaw, hasBlobEndpoint := secret.Data[cloud.AzureBlobEndpoint]

	if !hasAccountName && !hasBlobEndpoint {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' is not setup properly for azure.  It must have one '%s' or '%s'",
			g.Vdb.Spec.Communal.CredentialSecret, cloud.AzureAccountName, cloud.AzureBlobEndpoint)
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, ctrl.Result{Requeue: true}, nil
	}

	// The blob endpoint may have a protocol scheme as a prefix.  Strip that off
	// so its just the host and port.
	var blobEndpoint string
	if hasBlobEndpoint {
		blobEndpoint = getEndpointHostPort(string(blobEndpointRaw))
	}

	accountKey, hasAccountKey := secret.Data[cloud.AzureAccountKey]
	sas, hasSAS := secret.Data[cloud.AzureSharedAccessSignature]

	if hasAccountKey && hasSAS {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' is not setup properly for azure.  It cannot have both '%s' and '%s'",
			g.Vdb.Spec.Communal.CredentialSecret, cloud.AzureAccountKey, cloud.AzureSharedAccessSignature)
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
			Protocol:     getEndpointProtocol(string(blobEndpointRaw)),
		},
		ctrl.Result{}, nil
}

// getCommunalCredsSecret returns the contents of the communal credentials
// secret. It handles if the secret is not found and will log an event.
func (g *GenericDatabaseInitializer) getCommunalCredsSecret(ctx context.Context) (*corev1.Secret, ctrl.Result, error) {
	return getSecret(ctx, g.VRec, g.Vdb, names.GenCommunalCredSecretName(g.Vdb))
}

// getS3SseCustomerKeySecret returns the content of the customer key secret
// for server-side encryption. It handles if the secret is not found and will log an event.
func (g *GenericDatabaseInitializer) getS3SseCustomerKeySecret(ctx context.Context) (*corev1.Secret, ctrl.Result, error) {
	return getSecret(ctx, g.VRec, g.Vdb, names.GenS3SseCustomerKeySecretName(g.Vdb))
}

// getCommunalEndpoint get the communal endpoint for inclusion in the auth files.
// Takes the endpoint from vdb and strips off the protocol.
func (g *GenericDatabaseInitializer) getCommunalEndpoint() string {
	prefix := []string{"https://", "http://"}
	for _, pref := range prefix {
		if i := strings.Index(g.Vdb.Spec.Communal.Endpoint, pref); i == 0 {
			return strings.TrimSuffix(g.Vdb.Spec.Communal.Endpoint[len(pref):], "/")
		}
	}
	return g.Vdb.Spec.Communal.Endpoint
}

// getEnableHTTPS will return "1" if connecting to https otherwise return "0"
func (g *GenericDatabaseInitializer) getEnableHTTPS() string {
	if strings.HasPrefix(g.Vdb.Spec.Communal.Endpoint, "https://") {
		return "1"
	}
	return "0"
}

// getCAFile will return an entry for SystemCABundlePath if one needs to be included
func (g *GenericDatabaseInitializer) getCAFile() {
	if g.Vdb.Spec.Communal.CaFile != "" {
		g.ConfigurationParams["SystemCABundlePath"] = g.Vdb.Spec.Communal.CaFile
	}
}

// getServerSideEncryptionParmsContent adds server-side encryption related auth
// parms, if that is setup, to the config parms map. Must have encryption type in the Vdb.
func (g *GenericDatabaseInitializer) getServerSideEncryptionParmsContent(ctx context.Context) (reconcile.Result, error) {
	// Extract customer key from secret
	res, err := g.getS3SseCustomerKey(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.getServerSideEncryptionAlgorithm()
	g.getS3SseKmsKeyID()
	return ctrl.Result{}, nil
}

// getServerSideEncryptionAlgorithm adds an entry for S3ServerSideEncryption
// if sse type is SSE-S3|SSE-KMS or for S3SseCustomerAlgorithm if SSE-C
// to the config parms map.
func (g *GenericDatabaseInitializer) getServerSideEncryptionAlgorithm() {
	if g.Vdb.IsSseC() {
		g.ConfigurationParams[S3SseCustomerAlgorithm] = SseAlgorithmAES256
	}
	if g.Vdb.IsSseS3() {
		g.ConfigurationParams[S3ServerSideEncryption] = SseAlgorithmAES256
	}
	if g.Vdb.IsSseKMS() {
		g.ConfigurationParams[S3ServerSideEncryption] = SseAlgorithmAWSKMS
	}
}

// getS3SseKmsKeyID adds an entry for S3SseKmsKeyId to the config parms map
// when SSE-KMS is enabled.
func (g *GenericDatabaseInitializer) getS3SseKmsKeyID() {
	if g.Vdb.IsSseKMS() {
		g.ConfigurationParams[vapi.S3SseKmsKeyID] = g.Vdb.Spec.Communal.AdditionalConfig[vapi.S3SseKmsKeyID]
	}
}

// getS3SseCustomerKey adds an entry for S3SseCustomerKey to the config parms map,
// only when sse type is SSE-C
func (g *GenericDatabaseInitializer) getS3SseCustomerKey(ctx context.Context) (ctrl.Result, error) {
	if !g.Vdb.IsSseC() {
		return ctrl.Result{}, nil
	}

	secret, res, err := g.getS3SseCustomerKeySecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	clientKey, ok := secret.Data[cloud.S3SseCustomerKeyName]
	if !ok {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.S3SseCustomerWrongKey,
			"The s3SseCustomerKey secret '%s' does not have a key named '%s'",
			g.Vdb.Spec.Communal.S3SseCustomerKeySecret, cloud.S3SseCustomerKeyName)
		return ctrl.Result{Requeue: true}, nil
	}
	g.ConfigurationParams[S3SseCustomerKey] = string(clientKey)
	return ctrl.Result{}, nil
}

// getRegion adds an entry for region, to the config parms map, specific to the cloud provider
func (g *GenericDatabaseInitializer) getRegion(parmName string) {
	// We have a webhook to set the default value, but for legacy purposes we
	// always check for the empty string.
	if g.Vdb.Spec.Communal.Region == "" {
		g.ConfigurationParams[parmName] = vapi.DefaultS3Region
	}
	g.ConfigurationParams[parmName] = g.Vdb.Spec.Communal.Region
}

// getEndpointProtocol returns the protocol (HTTPS or HTTP) for the given endpoint
func getEndpointProtocol(blobEndpoint string) string {
	if blobEndpoint == "" {
		return cloud.AzureDefaultProtocol
	}
	re := regexp.MustCompile(`([a-z]+)://`)
	m := re.FindAllStringSubmatch(blobEndpoint, 1)
	if len(m) == 0 || len(m[0]) < 2 {
		return cloud.AzureDefaultProtocol
	}
	return strings.ToUpper(m[0][1])
}

// getEndpointHostPort returns just the host and port portion of a endpoint
func getEndpointHostPort(blobEndpoint string) string {
	re := regexp.MustCompile(`([a-z]+)://(.*)`)
	m := re.FindAllStringSubmatch(blobEndpoint, 1)
	if len(m) == 0 || len(m[0]) < 3 {
		return blobEndpoint
	}
	return strings.TrimSuffix(m[0][2], "/")
}

// getHadoopConfDir adds an entry to the config parms map for
// HadoopConfDir.  If that isn't present, an empty string is returned.
func (g *GenericDatabaseInitializer) getHadoopConfDir() {
	if g.Vdb.Spec.Communal.HadoopConfig != "" {
		g.ConfigurationParams["HadoopConfDir"] = paths.HadoopConfPath
	}
}

// hasCompatibleVersionForKerberos checks whether it has the required engine fix
// to run with a Kerberos config.  If it doesn't the ctrl.Result will have the
// requeue bool set.
func (g *GenericDatabaseInitializer) hasCompatibleVersionForKerberos() ctrl.Result {
	const DefaultKerberosSupportedVersion = "v11.0.2"
	eventMsg := genUnsupportedVerticaVersionEventMsg("Kerberos in the container", DefaultKerberosSupportedVersion)
	return g.hasCompatibleVersion(DefaultKerberosSupportedVersion, eventMsg)
}

// hasCompatibleVersionForServerSideEncryption checks whether it has the required engine fix
// to run with S3 server-side encryption.  If it doesn't the ctrl.Result will have the
// requeue bool set.
func (g *GenericDatabaseInitializer) hasCompatibleVersionForServerSideEncryption() ctrl.Result {
	const DefaultSseSupportedVersion = "v12.0.1"
	eventMsg := genUnsupportedVerticaVersionEventMsg("server side encryption", DefaultSseSupportedVersion)
	return g.hasCompatibleVersion(DefaultSseSupportedVersion, eventMsg)
}

// hasCompatibleVersion checks whether it has the required engine fix.
// If it doesn't the ctrl.Result will have the requeue bool set.
func (g *GenericDatabaseInitializer) hasCompatibleVersion(supportedVersion, eventMsg string) ctrl.Result {
	vinf, ok := g.Vdb.MakeVersionInfo()
	if !ok || ok && vinf.IsEqualOrNewer(supportedVersion) {
		return ctrl.Result{}
	}
	g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion, eventMsg, vinf.VdbVer)
	return ctrl.Result{Requeue: true}
}

// genLowerCaseKeysMap will generate a lowercase keys copy of
// ConfigurationParams map. This is useful to detect duplication
// between user-defined and operator-generated params.
func (g *GenericDatabaseInitializer) genLowerCaseKeysMap() map[string]string {
	parmNames := map[string]string{}
	for k, v := range g.ConfigurationParams {
		parmNames[strings.ToLower(k)] = v
	}
	return parmNames
}

// genUnsupportedVerticaVersionEventMsg returns a string that will be used as
// essage for 'UnsupportedVerticaVersion' event log.
func genUnsupportedVerticaVersionEventMsg(feature, supportedVersion string) string {
	// The '%s' is a placeholder for the version extracted from a vdb
	prefix := "The engine (%s) doesn't have the required change to setup"
	return fmt.Sprintf("%s %s. You must be on version %s or greater", prefix, feature, supportedVersion)
}
