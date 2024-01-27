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

package vdbconfig

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	vtypes "github.com/vertica/vertica-kubernetes/pkg/types"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

type ReconcilerInterface interface {
	Event(vdb runtime.Object, eventType string, reason string, message string)
	Eventf(vdb runtime.Object, eventType, reason, messageFmt string, args ...interface{})
	GetClient() client.Client
}

type VerticaReconciler struct {
	client.Client
	Log   logr.Logger
	EVRec record.EventRecorder
}

// GetClient gives access to the Kubernetes client
func (v *VerticaReconciler) GetClient() client.Client {
	return v.Client
}

// Event a wrapper for Event() that also writes a log entry
func (v *VerticaReconciler) Event(vdb runtime.Object, eventType, reason, message string) {
	evWriter := events.Writer{
		Log:   v.Log,
		EVRec: v.EVRec,
	}
	evWriter.Event(vdb, eventType, reason, message)
}

// Eventf is a wrapper for Eventf() that also writes a log entry
func (v *VerticaReconciler) Eventf(vdb runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	evWriter := events.Writer{
		Log:   v.Log,
		EVRec: v.EVRec,
	}
	evWriter.Eventf(vdb, eventtype, reason, messageFmt, args...)
}

// ConfigParamsGenerator can fill up incoming ConfigurationParams
type ConfigParamsGenerator struct {
	VRec                ReconcilerInterface
	Log                 logr.Logger
	Vdb                 *vapi.VerticaDB
	ConfigurationParams *vtypes.CiMap
	VInf                *version.Info
}

// ConstructConfigParms builds a map of all of the config parameters to use,
// and assigns the map to ConfigurationParams of ConfigParamsGenerator
func (g *ConfigParamsGenerator) ConstructConfigParms(ctx context.Context) (ctrl.Result, error) {
	if err := g.Setup(); err != nil {
		return ctrl.Result{}, err
	}
	var authConfigBuilder func(ctx context.Context) (ctrl.Result, error)
	if g.Vdb.Spec.Communal.Path == "" {
		g.Log.Info("Communal path is empty. Not setting up communal auth parms")
		return ctrl.Result{}, nil
	}
	if g.Vdb.IsS3() {
		authConfigBuilder = g.setS3AuthParms
	} else if g.Vdb.IsHDFS() {
		authConfigBuilder = g.setHDFSAuthParms
	} else if g.Vdb.IsGCloud() {
		authConfigBuilder = g.setGCloudAuthParms
	} else if g.Vdb.IsAzure() {
		authConfigBuilder = g.setAzureAuthParms
	} else {
		g.Log.Info("No special auth setup for communal path", "path", g.Vdb.Spec.Communal.Path)
	}
	var res ctrl.Result
	var err error
	if authConfigBuilder != nil {
		res, err = authConfigBuilder(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	if g.Vdb.HasKerberosConfig() {
		if res = g.hasCompatibleVersionForKerberos(); verrors.IsReconcileAborted(res, nil) {
			return res, nil
		}
		err = g.setKerberosAuthParms()
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	g.SetEncryptSpreadCommConfigIfNecessary()

	// In newer release, we moved what some config settings that use to be set
	// via SQL to config parms.
	if g.hasCompatibleVersion(vapi.DBSetupConfigParametersMinVersion) {
		g.setDefaultSubclusterConfig()
		g.setPreferredKSafetyConfig()
	}

	// Add any additional config parameters that were included in the CR.
	// To avoid duplicate values, if a parameter is already set through another CR field,
	// (like S3ServerSideEncryption through communal.s3ServerSideEncryption), the corresponding
	// key/value pair in this map is skipped.
	// This must be the last thing added to the config parms.
	if !g.Vdb.IsAdditionalConfigMapEmpty() {
		g.SetAdditionalConfigParms()
	}

	return ctrl.Result{}, nil
}

// GetConfigParms returns ConfiurationParams of ConfigParamsGenerator
// It is used after ConstructConfigParms(), and it can return a map of all config parameters
func (g *ConfigParamsGenerator) GetConfigParms() *vtypes.CiMap {
	return g.ConfigurationParams
}

// setup will initialize parms in the ConfigParamsGenerator struct
func (g *ConfigParamsGenerator) Setup() error {
	if g.ConfigurationParams == nil {
		g.ConfigurationParams = vtypes.MakeCiMap()
	}
	var err error
	g.VInf, err = g.Vdb.MakeVersionInfoCheck()
	return err
}

// setAuth adds the auth parms, if they exist, to the config parms map.
func (g *ConfigParamsGenerator) setAuth(ctx context.Context, parmName string) (ctrl.Result, error) {
	if g.Vdb.Spec.Communal.CredentialSecret == "" {
		return ctrl.Result{}, nil
	}

	// Extract the auth from the credential secret.
	auth, res, err := g.GetCommunalAuth(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.ConfigurationParams.Set(parmName, auth)
	return ctrl.Result{}, nil
}

// setS3AuthParms adds the auth parms to the config parms map when using S3
// communal storage.
func (g *ConfigParamsGenerator) setS3AuthParms(ctx context.Context) (ctrl.Result, error) {
	res, err := g.setAuth(ctx, "awsauth")
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	if g.Vdb.IsKnownSseType() {
		if res = g.hasCompatibleVersionForServerSideEncryption(); verrors.IsReconcileAborted(res, nil) {
			return res, nil
		}
		res, err = g.setServerSideEncryptionParms(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	g.ConfigurationParams.Set("awsendpoint", g.GetCommunalEndpoint())
	g.ConfigurationParams.Set("awsenablehttps", g.GetEnableHTTPS())
	g.setRegion(AWSRegionParm)
	g.setCAFile()
	return ctrl.Result{}, nil
}

// setHDFSAuthParms adds the auth parms to the config parms map when using
// HDFS communal storage.
func (g *ConfigParamsGenerator) setHDFSAuthParms(_ context.Context) (ctrl.Result, error) {
	g.setHadoopConfDir()
	g.setCAFile()
	return ctrl.Result{}, nil
}

// setKerberosAuthParms adds Kerberos related auth parms to the config parms map.
// Must have Kerberos config in the Vdb.
func (g *ConfigParamsGenerator) setKerberosAuthParms() error {
	// KerberosServiceName and KeberosRealm use to be separate parms in the CR.
	// They should now exist in AdditionalConfig.
	for _, key := range []string{meta.KerberosServiceNameConfig, meta.KerberosRealmConfig} {
		if _, ok := g.ConfigurationParams.Get(key); !ok {
			return fmt.Errorf("missing the %s config parameter in spec.communal.additionalConfig", key)
		}
	}
	g.ConfigurationParams.Set("KerberosKeytabFile", paths.Krb5Keytab)
	// We disable KerberosEnableKeytabPermissionCheck, otherwise the engine will
	// complain that the keytab file doesn't have read/write permissions from
	// dbadmin only.
	g.ConfigurationParams.Set("KerberosEnableKeytabPermissionCheck", "0")
	return nil
}

func (g *ConfigParamsGenerator) SetEncryptSpreadCommConfigIfNecessary() {
	if g.Vdb.Spec.EncryptSpreadComm != vapi.EncryptSpreadCommDisabled && g.hasCompatibleVersion(vapi.SetEncryptSpreadCommAsConfigVersion) {
		g.ConfigurationParams.Set("EncryptSpreadComm", g.Vdb.GetEncryptSpreadComm())
	}
}

func (g *ConfigParamsGenerator) setDefaultSubclusterConfig() {
	if g.Vdb.IsEON() {
		sc := g.Vdb.GetFirstPrimarySubcluster()
		g.ConfigurationParams.Set("InitialDefaultSubclusterName", sc.Name)
	}
}

func (g *ConfigParamsGenerator) setPreferredKSafetyConfig() {
	if g.Vdb.IsKSafety0() {
		g.ConfigurationParams.Set("InitialPreferredKSafe", "0")
	}
}

// setGCloudAuthParms adds the auth parms to the config parms map when we are
// connecting to google cloud storage.
func (g *ConfigParamsGenerator) setGCloudAuthParms(ctx context.Context) (ctrl.Result, error) {
	res, err := g.setAuth(ctx, "GCSAuth")
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.ConfigurationParams.Set("GCSEndpoint", g.GetCommunalEndpoint())
	g.ConfigurationParams.Set("GCSEnableHttps", g.GetEnableHTTPS())
	g.setRegion(GCloudRegionParm)
	g.setCAFile()
	return ctrl.Result{}, nil
}

// setAzureAuthParms adds the auth parms to the config parms map for an EON database created in
// Azure Blob Storage
func (g *ConfigParamsGenerator) setAzureAuthParms(ctx context.Context) (ctrl.Result, error) {
	if g.Vdb.Spec.Communal.CredentialSecret == "" {
		return ctrl.Result{}, nil
	}

	azureCreds, azureConfig, res, err := g.GetAzureAuth(ctx)
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

	g.ConfigurationParams.Set("AzureStorageCredentials", azureCredsJSON.String())
	g.ConfigurationParams.Set("AzureStorageEndpointConfig", azureConfigJSON.String())
	g.setCAFile()
	return ctrl.Result{}, nil
}

// setAdditionalConfigParms adds additional server config parameters
// to the config parms map.
func (g *ConfigParamsGenerator) SetAdditionalConfigParms() {
	for k, v := range g.Vdb.Spec.Communal.AdditionalConfig {
		_, ok := g.ConfigurationParams.Get(k)
		if ok {
			g.Log.Info("additional config parameter ignored", "parameter", k)
			continue
		}
		g.ConfigurationParams.Set(k, v)
	}
}

// getCommunalAuth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (g *ConfigParamsGenerator) GetCommunalAuth(ctx context.Context) (string, ctrl.Result, error) {
	secret, res, err := g.GetCommunalCredsSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return "", res, err
	}

	accessKey, ok := secret[cloud.CommunalAccessKeyName]
	if !ok {
		g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, cloud.CommunalAccessKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret[cloud.CommunalSecretKeyName]
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
func (g *ConfigParamsGenerator) GetAzureAuth(ctx context.Context) (
	cloud.AzureCredential, cloud.AzureEndpointConfig, ctrl.Result, error) {
	secretData, res, err := g.GetCommunalCredsSecret(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, res, err
	}

	accountName, hasAccountName := secretData[cloud.AzureAccountName]
	blobEndpointRaw, hasBlobEndpoint := secretData[cloud.AzureBlobEndpoint]

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
		blobEndpoint = GetEndpointHostPort(string(blobEndpointRaw))
	}

	accountKey, hasAccountKey := secretData[cloud.AzureAccountKey]
	sas, hasSAS := secretData[cloud.AzureSharedAccessSignature]

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
			Protocol:     GetEndpointProtocol(string(blobEndpointRaw)),
		},
		ctrl.Result{}, nil
}

// getCommunalCredsSecret returns the contents of the communal credentials
// secret. It handles if the secret is not found and will log an event.
func (g *ConfigParamsGenerator) GetCommunalCredsSecret(ctx context.Context) (map[string][]byte, ctrl.Result, error) {
	fetcher := cloud.VerticaDBSecretFetcher{
		Client:   g.VRec.GetClient(),
		Log:      g.Log,
		VDB:      g.Vdb,
		EVWriter: g.VRec,
	}
	return fetcher.FetchAllowRequeue(ctx, names.GenNamespacedName(g.Vdb, g.Vdb.Spec.Communal.CredentialSecret))
}

// getS3SseCustomerKeySecret returns the content of the customer key secret
// for server-side encryption. It handles if the secret is not found and will log an event.
func (g *ConfigParamsGenerator) GetS3SseCustomerKeySecret(ctx context.Context) (*corev1.Secret, ctrl.Result, error) {
	return getSecret(ctx, g.VRec, g.Vdb, names.GenS3SseCustomerKeySecretName(g.Vdb))
}

// getCommunalEndpoint get the communal endpoint for inclusion in the auth files.
// Takes the endpoint from vdb and strips off the protocol.
func (g *ConfigParamsGenerator) GetCommunalEndpoint() string {
	prefix := []string{"https://", "http://"}
	for _, pref := range prefix {
		if i := strings.Index(g.Vdb.Spec.Communal.Endpoint, pref); i == 0 {
			return strings.TrimSuffix(g.Vdb.Spec.Communal.Endpoint[len(pref):], "/")
		}
	}
	return g.Vdb.Spec.Communal.Endpoint
}

// getEnableHTTPS will return "1" if connecting to https otherwise return "0"
func (g *ConfigParamsGenerator) GetEnableHTTPS() string {
	if strings.HasPrefix(g.Vdb.Spec.Communal.Endpoint, "https://") {
		return "1"
	}
	return "0"
}

// setCAFile adds an entry for SystemCABundlePath, if one needs to be included,
// to the config parms map.
func (g *ConfigParamsGenerator) setCAFile() {
	if g.Vdb.Spec.Communal.CaFile != "" {
		g.ConfigurationParams.Set("SystemCABundlePath", g.Vdb.Spec.Communal.CaFile)
	}
}

// setServerSideEncryptionParms adds server-side encryption related config
// parms, if that is setup, to the config parms map. Must have encryption type in the Vdb.
func (g *ConfigParamsGenerator) setServerSideEncryptionParms(ctx context.Context) (reconcile.Result, error) {
	// Extract customer key from secret
	res, err := g.SetS3SseCustomerKey(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	g.SetServerSideEncryptionAlgorithm()
	g.setS3SseKmsKeyID()
	return ctrl.Result{}, nil
}

// setServerSideEncryptionAlgorithm adds an entry to the config parms map for S3ServerSideEncryption,
// if sse type is SSE-S3|SSE-KMS, or for S3SseCustomerAlgorithm, if sse type is SSE-C.
func (g *ConfigParamsGenerator) SetServerSideEncryptionAlgorithm() {
	switch {
	case g.Vdb.IsSseC():
		g.ConfigurationParams.Set(S3SseCustomerAlgorithm, SseAlgorithmAES256)
	case g.Vdb.IsSseS3():
		g.ConfigurationParams.Set(S3ServerSideEncryption, SseAlgorithmAES256)
	case g.Vdb.IsSseKMS():
		g.ConfigurationParams.Set(S3ServerSideEncryption, SseAlgorithmAWSKMS)
	}
}

// setS3SseKmsKeyID adds an entry for S3SseKmsKeyId to the config parms map
// when SSE-KMS is enabled.
func (g *ConfigParamsGenerator) setS3SseKmsKeyID() {
	if g.Vdb.IsSseKMS() {
		g.ConfigurationParams.Set(vapi.S3SseKmsKeyID, g.Vdb.Spec.Communal.AdditionalConfig[vapi.S3SseKmsKeyID])
	}
}

// setS3SseCustomerKey adds an entry for S3SseCustomerKey to the config parms map,
// only when sse type is SSE-C.
func (g *ConfigParamsGenerator) SetS3SseCustomerKey(ctx context.Context) (ctrl.Result, error) {
	if !g.Vdb.IsSseC() {
		return ctrl.Result{}, nil
	}

	secret, res, err := g.GetS3SseCustomerKeySecret(ctx)
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
	g.ConfigurationParams.Set(S3SseCustomerKey, string(clientKey))
	return ctrl.Result{}, nil
}

// setRegion adds an entry for region, to the config parms map, specific to the cloud provider
func (g *ConfigParamsGenerator) setRegion(parmName string) {
	// We have a webhook to set the default value, but for legacy purposes we
	// always check for the empty string.
	if g.Vdb.Spec.Communal.Region == "" {
		g.ConfigurationParams.Set(parmName, vapi.DefaultS3Region)
	}
	g.ConfigurationParams.Set(parmName, g.Vdb.Spec.Communal.Region)
}

// GetEndpointProtocol returns the protocol (HTTPS or HTTP) for the given endpoint
func GetEndpointProtocol(blobEndpoint string) string {
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
func GetEndpointHostPort(blobEndpoint string) string {
	re := regexp.MustCompile(`([a-z]+)://(.*)`)
	m := re.FindAllStringSubmatch(blobEndpoint, 1)
	if len(m) == 0 || len(m[0]) < 3 {
		return blobEndpoint
	}
	return strings.TrimSuffix(m[0][2], "/")
}

// setHadoopConfDir adds an entry to the config parms map for
// HadoopConfDir. Must have the corresponding config map set in the Vdb.
func (g *ConfigParamsGenerator) setHadoopConfDir() {
	if g.Vdb.Spec.HadoopConfig != "" {
		g.ConfigurationParams.Set("HadoopConfDir", paths.HadoopConfPath)
	}
}

// hasCompatibleVersionForKerberos checks whether it has the required engine fix
// to run with a Kerberos config.  If it doesn't the ctrl.Result will have the
// requeue bool set.
func (g *ConfigParamsGenerator) hasCompatibleVersionForKerberos() ctrl.Result {
	const DefaultKerberosSupportedVersion = "v11.0.2"
	eventMsg := genUnsupportedVerticaVersionEventMsg("Kerberos in the container", DefaultKerberosSupportedVersion)
	return g.hasCompatibleVersionElseRequeue(DefaultKerberosSupportedVersion, eventMsg)
}

// hasCompatibleVersionForServerSideEncryption checks whether it has the required engine fix
// to run with S3 server-side encryption.  If it doesn't the ctrl.Result will have the
// requeue bool set.
func (g *ConfigParamsGenerator) hasCompatibleVersionForServerSideEncryption() ctrl.Result {
	const DefaultSseSupportedVersion = "v12.0.1"
	eventMsg := genUnsupportedVerticaVersionEventMsg("server side encryption", DefaultSseSupportedVersion)
	return g.hasCompatibleVersionElseRequeue(DefaultSseSupportedVersion, eventMsg)
}

// hasCompatibleVersionElseRequeue checks whether it has the required engine fix.
// If it doesn't an event message is logged and the ctrl.Result will have the
// requeue bool set.
func (g *ConfigParamsGenerator) hasCompatibleVersionElseRequeue(supportedVersion, eventMsg string) ctrl.Result {
	if g.hasCompatibleVersion(supportedVersion) {
		return ctrl.Result{}
	}
	g.VRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion, eventMsg, supportedVersion)
	return ctrl.Result{Requeue: true}
}

// hasCompatibleVersion checks whether it has the required engine fix returning a bool.
func (g *ConfigParamsGenerator) hasCompatibleVersion(supportedVersion string) bool {
	return g.VInf.IsEqualOrNewer(supportedVersion)
}

// genUnsupportedVerticaVersionEventMsg returns a string that will be used as
// message for 'UnsupportedVerticaVersion' event log.
func genUnsupportedVerticaVersionEventMsg(feature, supportedVersion string) string {
	// The '%s' is a placeholder for the version extracted from a vdb
	prefix := "The engine (%s) doesn't have the required change to setup"
	return fmt.Sprintf("%s %s. You must be on version %s or greater", prefix, feature, supportedVersion)
}

func getSecret(ctx context.Context, vrec ReconcilerInterface, vdb1 *vapi.VerticaDB,
	nm types.NamespacedName) (*corev1.Secret, ctrl.Result, error) {
	secret := &corev1.Secret{}
	res, err := getConfigMapOrSecret(ctx, vrec, vdb1, nm, secret)
	return secret, res, err
}

// getConfigMapOrSecret is a generic function to fetch a ConfigMap or a Secret.
// It will handle logging an event if the configMap or secret is missing.
func getConfigMapOrSecret(ctx context.Context, vrec ReconcilerInterface, vdb1 *vapi.VerticaDB,
	nm types.NamespacedName, obj client.Object) (ctrl.Result, error) {
	if err := vrec.GetClient().Get(ctx, nm, obj); err != nil {
		if errors.IsNotFound(err) {
			objType := ""
			switch v := obj.(type) {
			default:
				objType = fmt.Sprintf("%T", v)
			case *corev1.Secret:
				objType = "Secret"
			case *corev1.ConfigMap:
				objType = "ConfigMap"
			}
			vrec.Eventf(vdb1, corev1.EventTypeWarning, events.ObjectNotFound,
				"Could not find the %s '%s'", objType, nm)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{},
			fmt.Errorf("could not read the secret %s: %w", nm, err)
	}
	return ctrl.Result{}, nil
}
