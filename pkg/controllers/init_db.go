/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/go-logr/logr"
	"github.com/lithammer/dedent"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	AWSRegionParm    = "awsregion"
	GCloudRegionParm = "GCSRegion"
)

type DatabaseInitializer interface {
	getPodList() ([]*PodFact, bool)
	genCmd(ctx context.Context, hostList []string) ([]string, error)
	execCmd(ctx context.Context, atPod types.NamespacedName, cmd []string) (ctrl.Result, error)
	preCmdSetup(ctx context.Context, atPod types.NamespacedName) error
}

type GenericDatabaseInitializer struct {
	initializer DatabaseInitializer
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB
	PRunner     cmds.PodRunner
	PFacts      *PodFacts
}

// checkAndRunInit will check if the database needs to be initialized and run init if applicable
func (g *GenericDatabaseInitializer) checkAndRunInit(ctx context.Context) (ctrl.Result, error) {
	if err := g.PFacts.Collect(ctx, g.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if exists := g.PFacts.doesDBExist(); exists.IsFalse() {
		res, err := g.runInit(ctx)
		if err != nil || res.Requeue {
			return res, err
		}
	} else if exists.IsNone() {
		// Could not determine if DB didn't exist.  Missing state with some of the pods.
		return ctrl.Result{Requeue: true}, nil
	}

	return ctrl.Result{}, nil
}

// runInit will physically setup the database.
// Depending on g.initializer, this will either do create_db or revive_db.
func (g *GenericDatabaseInitializer) runInit(ctx context.Context) (ctrl.Result, error) {
	atPodFact, ok := g.PFacts.findPodToRunAdmintools()
	if !ok {
		// Could not find a runable pod to run from.
		return ctrl.Result{Requeue: true}, nil
	}
	atPod := atPodFact.name

	if res, err := g.ConstructAuthParms(ctx, atPod); err != nil || res.Requeue {
		return res, err
	}
	if err := g.initializer.preCmdSetup(ctx, atPod); err != nil {
		return ctrl.Result{}, err
	}

	podList, ok := g.initializer.getPodList()
	if !ok {
		// Was not able to generate the pod list
		return ctrl.Result{Requeue: true}, nil
	}
	ok = g.checkPodList(podList)
	if !ok {
		g.Log.Info("Aborting reconiliation as not all of required pods are running")
		return ctrl.Result{Requeue: true}, nil
	}

	// Cleanup for any prior failed attempt.
	if err := g.cleanupLocalFilesInPods(ctx, podList); err != nil {
		return ctrl.Result{}, err
	}

	if err := changeDepotPermissions(ctx, g.Vdb, g.PRunner, podList); err != nil {
		return ctrl.Result{}, err
	}

	debugDumpAdmintoolsConf(ctx, g.PRunner, atPod)

	cmd, err := g.initializer.genCmd(ctx, getHostList(podList))
	if err != nil {
		return ctrl.Result{}, err
	}
	if res, err := g.initializer.execCmd(ctx, atPod, cmd); err != nil || res.Requeue {
		return res, err
	}

	debugDumpAdmintoolsConf(ctx, g.PRunner, atPod)

	cond := vapi.VerticaDBCondition{Type: vapi.DBInitialized, Status: corev1.ConditionTrue}
	if err := status.UpdateCondition(ctx, g.VRec.Client, g.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}

	if err := g.DestroyAuthParms(ctx, atPod); err != nil {
		// Destroying the auth parms is a best effort. If we fail to delete it,
		// the reconcile will continue on.
		g.Log.Info("failed to destroy auth parms, ignoring failure", "err", err)
	}

	// The DB has been initialized. We invalidate the cache now so that next
	// access will refresh with the new db state. A status reconciler will
	// follow this that will update the Vdb status about the db existence.
	g.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// getHostList will return a host list from the given pods
func getHostList(podList []*PodFact) []string {
	hostList := make([]string, 0, len(podList))
	for _, pod := range podList {
		hostList = append(hostList, pod.podIP)
	}
	return hostList
}

// checkPodList ensures all of the pods that we will use for the init call are running
func (g *GenericDatabaseInitializer) checkPodList(podList []*PodFact) bool {
	for _, pod := range podList {
		// Bail if find one of the pods isn't running
		if !pod.isPodRunning {
			return false
		}
	}
	return true
}

// cleanupLocalFilesInPods will go through each pod and ensure their local files are gone.
// This step is necessary because a failed create_db can leave old state around.
func (g *GenericDatabaseInitializer) cleanupLocalFilesInPods(ctx context.Context, podList []*PodFact) error {
	for _, pod := range podList {
		// Cleanup any local paths. This step is needed if an earlier create_db
		// fails -- admintools does not clean everything up.
		if err := cleanupLocalFiles(ctx, g.Vdb, g.PRunner, pod.name); err != nil {
			return err
		}
	}
	return nil
}

// ConstructAuthParms builds the authentication parms and ensure it exists in the pod
func (g *GenericDatabaseInitializer) ConstructAuthParms(ctx context.Context, atPod types.NamespacedName) (ctrl.Result, error) {
	var contentGen func(ctx context.Context) (string, ctrl.Result, error)

	if g.Vdb.IsS3() {
		contentGen = g.getS3AuthParmsContent
	} else if g.Vdb.IsHDFS() {
		contentGen = g.getHDFSAuthParmsContent
	} else if g.Vdb.IsGCloud() {
		contentGen = g.getGCloudAuthParmsContent
	} else if g.Vdb.IsAzure() {
		contentGen = g.getAzureAuthParmsContent
	} else {
		err := fmt.Errorf("unknown communal storage type: '%s'", g.Vdb.Spec.Communal.Path)
		g.Log.Error(err, "unable to create auth parms for communal type")
		return ctrl.Result{}, err
	}

	content, res, err := contentGen(ctx)
	if res.Requeue || err != nil {
		return res, err
	}

	err = g.copyAuthFile(ctx, atPod, content)
	return ctrl.Result{}, err
}

// DestroyAuthParms will remove the auth parms file that was created in the pod
func (g *GenericDatabaseInitializer) DestroyAuthParms(ctx context.Context, atPod types.NamespacedName) error {
	_, _, err := g.PRunner.ExecInPod(ctx, atPod, names.ServerContainer,
		"rm", paths.AuthParmsFile,
	)
	return err
}

// getS3AuthParmsContent construct a string for the auth parms when using S3
// communal storage.
func (g *GenericDatabaseInitializer) getS3AuthParmsContent(ctx context.Context) (string, ctrl.Result, error) {
	// Extract the auth from the credential secret.
	auth, res, err := g.getCommunalAuth(ctx)
	if err != nil || res.Requeue {
		return "", res, err
	}

	content := fmt.Sprintf(`
			awsauth = %s
			awsendpoint = %s
			awsenablehttps = %s
			%s
			%s
		`, auth, g.getCommunalEndpoint(), g.getEnableHTTPS(), g.getRegion(AWSRegionParm), g.getCAFile(),
	)
	return dedent.Dedent(content), ctrl.Result{}, nil
}

// getHDFSAuthParmsContent construct a string for the auth parms when using
// HDFS communal storage.
func (g *GenericDatabaseInitializer) getHDFSAuthParmsContent(ctx context.Context) (string, ctrl.Result, error) {
	if g.Vdb.Spec.Communal.HadoopConfig != "" {
		content := fmt.Sprintf(`
			HadoopConfDir = %s
		`, paths.HadoopConfPath)
		return dedent.Dedent(content), ctrl.Result{}, nil
	}

	return "", ctrl.Result{}, nil
}

// getGCloudAuthParmsContent will get the content for the auth parms when we are
// connecting to google cloud storage.
func (g *GenericDatabaseInitializer) getGCloudAuthParmsContent(ctx context.Context) (string, ctrl.Result, error) {
	// Extract the auth from the credential secret.
	auth, res, err := g.getCommunalAuth(ctx)
	if err != nil || res.Requeue {
		return "", res, err
	}

	content := fmt.Sprintf(`
		GCSAuth = %s
		GCSEndpoint = %s
		GCSEnableHttps = %s
		%s
	`, auth, g.getCommunalEndpoint(), g.getEnableHTTPS(), g.getRegion(GCloudRegionParm))
	return dedent.Dedent(content), ctrl.Result{}, nil
}

// getAzureAuthParmsContent will get the content for an EON database created in
// Azure Blob Storage
func (g *GenericDatabaseInitializer) getAzureAuthParmsContent(ctx context.Context) (string, ctrl.Result, error) {
	azureCreds, azureConfig, res, err := g.getAzureAuth(ctx)
	if err != nil || res.Requeue {
		return "", res, err
	}

	var azureCredsJSON strings.Builder
	elemPrefix := ""
	azureCredsJSON.WriteString("[{")
	if azureCreds.AccountName != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`"accountName": "%s"`, azureCreds.AccountName))
		elemPrefix = ","
	}
	if azureCreds.BlobEndpoint != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"blobEndpoint": "%s"`, elemPrefix, azureCreds.BlobEndpoint))
		elemPrefix = ","
	}
	if azureCreds.AccountKey != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"accountKey": "%s"`, elemPrefix, azureCreds.AccountKey))
		elemPrefix = ","
	}
	if azureCreds.SharedAccessSignature != "" {
		azureCredsJSON.WriteString(fmt.Sprintf(`%s"sharedAccessSignature": "%s"`, elemPrefix, azureCreds.SharedAccessSignature))
	}
	azureCredsJSON.WriteString("}]")

	var azureConfigJSON strings.Builder
	elemPrefix = ""
	azureConfigJSON.WriteString("[{")
	if azureConfig.AccountName != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`"accountName": "%s"`, azureConfig.AccountName))
		elemPrefix = ","
	}
	if azureConfig.BlobEndpoint != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`%s"blobEndpoint": "%s"`, elemPrefix, azureConfig.BlobEndpoint))
		elemPrefix = ","
	}
	if azureConfig.Protocol != "" {
		azureConfigJSON.WriteString(fmt.Sprintf(`%s"protocol": "%s"`, elemPrefix, azureConfig.Protocol))
		elemPrefix = ","
	}
	azureConfigJSON.WriteString(fmt.Sprintf(`%s"isMultiAccountEndpoint": %t`, elemPrefix, azureConfig.IsMultiAccountEndpoint))
	azureConfigJSON.WriteString("}]")

	content := fmt.Sprintf(`
	  AzureStorageCredentials = %s
	  AzureStorageEndpointConfig = %s
	`, azureCredsJSON.String(), azureConfigJSON.String())
	return dedent.Dedent(content), ctrl.Result{}, nil
}

// copyAuthFile will copy the auth file into the container
func (g *GenericDatabaseInitializer) copyAuthFile(ctx context.Context, atPod types.NamespacedName, content string) error {
	_, _, err := g.PRunner.ExecInPod(ctx, atPod, names.ServerContainer,
		"bash", "-c", fmt.Sprintf("cat > %s<<< '%s'", paths.AuthParmsFile, content))

	// We log an event for this error because it could be caused by bad values
	// in the creds.  If the value we get out of the secret has undisplayable
	// characters then we won't even be able to copy the file.
	if err != nil {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.AuthParmsCopyFailed,
			"Failed to copy auth parms to the pod '%s'", atPod)
	}
	return err
}

// getCommunalAuth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (g *GenericDatabaseInitializer) getCommunalAuth(ctx context.Context) (string, ctrl.Result, error) {
	secret, res, err := g.getCommunalCredsSecret(ctx)
	if res.Requeue || err != nil {
		return "", res, err
	}

	accessKey, ok := secret.Data[CommunalAccessKeyName]
	if !ok {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, CommunalAccessKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret.Data[CommunalSecretKeyName]
	if !ok {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' does not have a key named '%s'", g.Vdb.Spec.Communal.CredentialSecret, CommunalSecretKeyName)
		return "", ctrl.Result{Requeue: true}, nil
	}

	auth := fmt.Sprintf("%s:%s", strings.TrimSuffix(string(accessKey), "\n"),
		strings.TrimSuffix(string(secretKey), "\n"))
	return auth, ctrl.Result{}, nil
}

// getAzureAuth gets the azure credentials from the communal auth secret
func (g *GenericDatabaseInitializer) getAzureAuth(ctx context.Context) (AzureCredential, AzureEndpointConfig, ctrl.Result, error) {
	secret, res, err := g.getCommunalCredsSecret(ctx)
	if res.Requeue || err != nil {
		return AzureCredential{}, AzureEndpointConfig{}, res, err
	}

	accountName, hasAccountName := secret.Data[AzureAccountName]
	blobEndpointRaw, hasBlobEndpoint := secret.Data[AzureBlobEndpoint]

	if !hasAccountName && !hasBlobEndpoint {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' is not setup properly for azure.  It must have one '%s' or '%s'",
			g.Vdb.Spec.Communal.CredentialSecret, AzureAccountName, AzureBlobEndpoint)
		return AzureCredential{}, AzureEndpointConfig{}, ctrl.Result{Requeue: true}, nil
	}

	// The blob endpoint may have a protocol scheme as a prefix.  Strip that off
	// so its just the host and port.
	var blobEndpoint string
	if hasBlobEndpoint {
		blobEndpoint = getEndpointHostPort(string(blobEndpointRaw))
	}

	accountKey, hasAccountKey := secret.Data[AzureAccountKey]
	sas, hasSAS := secret.Data[AzureSharedAccessSignature]

	if (!hasAccountKey && !hasSAS) || (hasAccountKey && hasSAS) {
		g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The communal credential secret '%s' is not setup properly for azure.  It must have one '%s' or '%s'",
			g.Vdb.Spec.Communal.CredentialSecret, AzureAccountKey, AzureSharedAccessSignature)
		return AzureCredential{}, AzureEndpointConfig{}, ctrl.Result{Requeue: true}, nil
	}

	return AzureCredential{
			AccountName:           string(accountName),
			BlobEndpoint:          blobEndpoint,
			AccountKey:            string(accountKey),
			SharedAccessSignature: string(sas),
		},
		AzureEndpointConfig{
			AccountName:            string(accountName),
			BlobEndpoint:           blobEndpoint,
			Protocol:               getEndpointProtocol(string(blobEndpointRaw)),
			IsMultiAccountEndpoint: false,
		},
		ctrl.Result{}, nil
}

// getCommunalCredsSecret returns the contents of the communal credentials
// secret.  It handles if the secret is not found and will log an event.
func (g *GenericDatabaseInitializer) getCommunalCredsSecret(ctx context.Context) (*corev1.Secret, ctrl.Result, error) {
	secret := &corev1.Secret{}
	if err := g.VRec.Client.Get(ctx, names.GenCommunalCredSecretName(g.Vdb), secret); err != nil {
		if errors.IsNotFound(err) {
			g.VRec.EVRec.Eventf(g.Vdb, corev1.EventTypeWarning, events.CommunalCredsNotFound,
				"Could not find the communal credential secret '%s'", g.Vdb.Spec.Communal.CredentialSecret)
			return &corev1.Secret{}, ctrl.Result{Requeue: true}, nil
		}
		return &corev1.Secret{}, ctrl.Result{},
			fmt.Errorf("could not read the communal credential secret %s: %w", g.Vdb.Spec.Communal.CredentialSecret, err)
	}
	return secret, ctrl.Result{}, nil
}

// getCommunalEndpoint get the communal endpoint for inclusion in the auth files.
// Takes the endpoint from vdb and strips off the protocol.
func (g *GenericDatabaseInitializer) getCommunalEndpoint() string {
	prefix := []string{"https://", "http://"}
	for _, pref := range prefix {
		if i := strings.Index(g.Vdb.Spec.Communal.Endpoint, pref); i == 0 {
			return g.Vdb.Spec.Communal.Endpoint[len(pref):]
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

// getCAFile will return an entry for awscafile if one needs to be included
func (g *GenericDatabaseInitializer) getCAFile() string {
	if g.Vdb.Spec.Communal.CaFile == "" {
		return ""
	}
	return fmt.Sprintf("awscafile = %s", g.Vdb.Spec.Communal.CaFile)
}

// getRegion will return an entry for region, specific to the cloud provider
func (g *GenericDatabaseInitializer) getRegion(parmName string) string {
	// We have a webhook to set the default value, but for legacy purposes we
	// always check for the empty string.
	if g.Vdb.Spec.Communal.Region == "" {
		return fmt.Sprintf("%s = %s", parmName, vapi.DefaultS3Region)
	}
	return fmt.Sprintf("%s = %s", parmName, g.Vdb.Spec.Communal.Region)
}

// getEndpointProtocol returns the protocol (HTTPS or HTTP) for the given endpoint
func getEndpointProtocol(blobEndpoint string) string {
	if blobEndpoint == "" {
		return AzureDefaultProtocol
	}
	re := regexp.MustCompile(`([a-z]+)://`)
	m := re.FindAllStringSubmatch(blobEndpoint, 1)
	if len(m) == 0 || len(m[0]) < 2 {
		return AzureDefaultProtocol
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
	return m[0][2]
}
