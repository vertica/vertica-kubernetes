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
	"sort"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/license"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	vtypes "github.com/vertica/vertica-kubernetes/pkg/types"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// This is a file that we run with the create_db to run custome SQL. This is
	// passed with the --sql parameter when running create_db. This is no longer
	// used starting with versions defined in vapi.DBSetupConfigParameters.
	PostDBCreateSQLFile            = "/home/dbadmin/post-db-create.sql"
	PostDBCreateSQLFileVclusterOps = "/tmp/post-db-create.sql"
)

// CreateDBReconciler will create a database if one wasn't created yet.
type CreateDBReconciler struct {
	VRec                *VerticaDBReconciler
	Log                 logr.Logger
	Vdb                 *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner             cmds.PodRunner
	PFacts              *podfacts.PodFacts
	Dispatcher          vadmin.Dispatcher
	ConfigurationParams *vtypes.CiMap
	VInf                *version.Info
}

// MakeCreateDBReconciler will build a CreateDBReconciler object
func MakeCreateDBReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts,
	dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &CreateDBReconciler{
		VRec:                vdbrecon,
		Log:                 log.WithName("CreateDBReconciler"),
		Vdb:                 vdb,
		PRunner:             prunner,
		PFacts:              pfacts,
		Dispatcher:          dispatcher,
		ConfigurationParams: vtypes.MakeCiMap(),
	}
}

// Reconcile will ensure a DB exists and create one if it doesn't
func (c *CreateDBReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Skip this reconciler entirely if the init policy is not to create the DB.
	if c.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyCreate &&
		c.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyCreateSkipPackageInstall {
		return ctrl.Result{}, nil
	}

	var err error
	c.VInf, err = c.Vdb.MakeVersionInfoCheck()
	if err != nil {
		// The version should be in the VerticaDB. Although it could be missing
		// if we have a cached copy of the VerticaDB that is from prior to the
		// annotation update. Requeue to force a new reconciliation to read
		// latest copy.
		return ctrl.Result{}, err
	}

	// The remaining create_db logic is driven from GenericDatabaseInitializer.
	// This exists to creation an abstraction that is common with revive_db.
	g := GenericDatabaseInitializer{
		initializer: c,
		PRunner:     c.PRunner,
		PFacts:      c.PFacts,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec:                c.VRec,
			Log:                 c.Log,
			Vdb:                 c.Vdb,
			ConfigurationParams: c.ConfigurationParams,
		},
	}
	return g.checkAndRunInit(ctx)
}

// execCmd will do the actual execution of creating a database.
// This handles logging of necessary events.
func (c *CreateDBReconciler) execCmd(ctx context.Context, initiatorPod types.NamespacedName,
	hostList []string, podNames []types.NamespacedName) (ctrl.Result, error) {
	opts, err := c.genOptions(ctx, initiatorPod, podNames, hostList)
	if err != nil {
		return ctrl.Result{}, err
	}
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal, events.CreateDBStart, "Starting create database")

	start := time.Now()
	if res, err := c.Dispatcher.CreateDB(ctx, opts...); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if c.Vdb.IsCertRotationEnabled() {
		cmd := []string{
			"-f", PostDBCreateSQLFileVclusterOps,
		}
		_, stderr, err2 := c.PRunner.ExecVSQL(ctx, initiatorPod, names.ServerContainer, cmd...)
		if err2 != nil || strings.Contains(stderr, "Error") {
			c.Log.Error(err2, "failed to execute TLS DDLs after db creation stderr - "+stderr)
			return ctrl.Result{}, err2
		}
		chgs := vk8s.MetaChanges{
			NewAnnotations: map[string]string{
				vmeta.NMAHTTPSPreviousSecret: c.Vdb.Spec.NMATLSSecret,
			},
		}
		if _, err := vk8s.MetaUpdate(ctx, c.VRec.Client, c.Vdb.ExtractNamespacedName(), c.Vdb, chgs); err != nil {
			return ctrl.Result{}, err
		}
		c.Log.Info("TLS DDLs executed and TLS Cert configured")
	}
	sc := c.getFirstPrimarySubcluster()
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateDBSucceeded,
		"Successfully created database with subcluster '%s'. It took %s", sc.Name, time.Since(start).Truncate(time.Second))
	return ctrl.Result{}, nil
}

// preCmdSetup will generate the file we include with the create_db.
// This file runs any custom SQL for the create_db.
func (c *CreateDBReconciler) preCmdSetup(ctx context.Context, initiatorPod types.NamespacedName,
	_ string) (ctrl.Result, error) {
	// If the communal path is a POSIX file path, we need to create the communal
	// path directory as the server won't create it. It handles that for other
	// communal types though.
	if c.Vdb.Spec.Communal.Path != "" && !c.Vdb.IsKnownCommunalPrefix() {
		// We intentionally skip any errors. If there is an error creating the
		// directory, this will manifest itself later when we attempt the
		// created. That error will have better reporting than if we were
		// handle it here.
		_, _, _ = c.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
			"bash", "-c", fmt.Sprintf("mkdir -p %s", c.Vdb.GetCommunalPath()),
		)
	}

	// On older versions of vertica we need to drive a restart if setting
	// encryptSpreadComm. Set a condition variable so this happens after the
	// create.
	if c.Vdb.Spec.EncryptSpreadComm != vapi.EncryptSpreadCommDisabled && c.VInf.IsOlder(vapi.SetEncryptSpreadCommAsConfigVersion) {
		c.Log.Info("Setting restart needed status condition", "encryptSpreadComm", c.Vdb.Spec.EncryptSpreadComm)
		cond := vapi.MakeCondition(vapi.VerticaRestartNeeded, metav1.ConditionTrue, "SpreadCommEncryptionEnabled")
		if err := vdbstatus.UpdateCondition(ctx, c.VRec.Client, c.Vdb, cond); err != nil {
			return ctrl.Result{}, err
		}
	}

	return c.generatePostDBCreateSQL(ctx, initiatorPod)
}

// GetEndpoint gets the endpoint from the endpoint URL and strips off the protocol.
func (c *CreateDBReconciler) GetEndpoint(endPoint string) string {
	if endPoint == "" {
		return ""
	}
	prefix := []string{"https://", "http://"}
	for _, pref := range prefix {
		if i := strings.Index(endPoint, pref); i == 0 {
			return strings.TrimSuffix(endPoint[len(pref):], "/")
		}
	}
	return endPoint
}

// GetBucket returns the bucket name from the path URL
func (c *CreateDBReconciler) GetBucket(path string) string {
	if path == "" {
		return ""
	}

	return strings.TrimLeft(strings.TrimRight(path, "/"), "//")
}

// GetCredsSecret returns the contents of the credentials
// secret. It handles if the secret is not found and will log an event.
func (c *CreateDBReconciler) GetCredsSecret(ctx context.Context, credsSecret string) (map[string][]byte, ctrl.Result, error) {
	fetcher := cloud.SecretFetcher{
		Client:   c.VRec.GetClient(),
		Log:      c.Log,
		Obj:      c.Vdb,
		EVWriter: c.VRec,
	}
	return fetcher.FetchAllowRequeue(ctx, names.GenNamespacedName(c.Vdb, credsSecret))
}

// getAuth will return the access key and secret key.
// Value is returned in the format: <accessKey>:<secretKey>
func (c *CreateDBReconciler) GetAuth(ctx context.Context, credsSecret string) (string, string, ctrl.Result, error) {
	secret, res, err := c.GetCredsSecret(ctx, credsSecret)
	if verrors.IsReconcileAborted(res, err) {
		return "", "", res, err
	}

	accessKey, ok := secret[cloud.CommunalAccessKeyName]
	if !ok {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' does not have a key named '%s'", credsSecret, cloud.CommunalAccessKeyName)
		return "", "", ctrl.Result{Requeue: true}, nil
	}

	secretKey, ok := secret[cloud.CommunalSecretKeyName]
	if !ok {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
			"The credential secret '%s' does not have a key named '%s'", credsSecret, cloud.CommunalSecretKeyName)
		return "", "", ctrl.Result{Requeue: true}, nil
	}

	return string(accessKey), string(secretKey), ctrl.Result{}, nil
}

// getAzureAuth gets the azure credentials from the communal auth secret
func (c *CreateDBReconciler) GetAzureAuth(ctx context.Context, credsSecret string) (
	cloud.AzureCredential, cloud.AzureEndpointConfig, ctrl.Result, error) {
	secretData, res, err := c.GetCredsSecret(ctx, credsSecret)
	if verrors.IsReconcileAborted(res, err) {
		return cloud.AzureCredential{}, cloud.AzureEndpointConfig{}, res, err
	}

	accountName, hasAccountName := secretData[cloud.AzureAccountName]
	blobEndpointRaw, hasBlobEndpoint := secretData[cloud.AzureBlobEndpoint]

	if !hasAccountName && !hasBlobEndpoint {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
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
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CommunalCredsWrongKey,
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

// generatePostDBCreateSQL is a function that creates a file with sql commands
// to be run immediately after the database create.
func (c *CreateDBReconciler) generatePostDBCreateSQL(ctx context.Context, initiatorPod types.NamespacedName) (ctrl.Result, error) {
	cmd := ""
	// If version is older than DBSetupConfigParametersMinVersion or newer than vapi.TLSCertRotationMinVersion,
	// run SQL after DB creation. Otherwise, skip this function
	if c.VInf.IsEqualOrNewer(vapi.DBSetupConfigParametersMinVersion) && !c.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}
	// We include SQL to rename the default subcluster to match the name of the
	// first subcluster in the spec -- any remaining subclusters will be added
	// by DBAddSubclusterReconciler.
	sc := c.getFirstPrimarySubcluster()
	var sb strings.Builder
	sb.WriteString("-- SQL that is run after the database is created\n")
	if c.VInf.IsOlder(vapi.DBSetupConfigParametersMinVersion) {
		if c.Vdb.IsEON() {
			sb.WriteString(
				fmt.Sprintf(`alter subcluster default_subcluster rename to \"%s\";`, sc.Name),
			)
		}
		if c.Vdb.IsKSafety0() {
			sb.WriteString("select set_preferred_ksafe(0);\n")
		}
		// On newer vertica versions, the EncrpytSpreadComm setting can be set as a
		// config parm in the create db call.
		if c.Vdb.Spec.EncryptSpreadComm != vapi.EncryptSpreadCommDisabled && c.VInf.IsOlder(vapi.SetEncryptSpreadCommAsConfigVersion) {
			sb.WriteString(fmt.Sprintf(`alter database default set parameter EncryptSpreadComm = '%s';
			`, vapi.EncryptSpreadCommWithVertica))
		}
		cmd = "cat > " + PostDBCreateSQLFile + "<<< \"" + sb.String() + "\""
	}
	if c.Vdb.IsCertRotationEnabled() {
		switch {
		case secrets.IsGSMSecret(c.Vdb.Spec.NMATLSSecret):
			return ctrl.Result{}, nil
		case secrets.IsAWSSecretsManagerSecret(c.Vdb.Spec.NMATLSSecret):
			c.generateAWSTlsSQL(&sb)
		default:
			c.generateKubernetesTLSSQL(&sb)
		}
		sb.WriteString(`select sync_catalog();`)
		cmd = "cat > " + PostDBCreateSQLFileVclusterOps + "<<< " + escapeForBash(sb.String())
	}

	if c.Vdb.HasAdditionalBuckets() {
		for _, bucket := range c.Vdb.Spec.AdditionalBuckets {
			// Extract the auth from the credential secret.
			accessKey, secretKey, res, err := c.GetAuth(ctx, bucket.CredentialSecret)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}

			if strings.HasPrefix(bucket.Path, v1.S3Prefix) {
				sb.WriteString(fmt.Sprintf(
					`ALTER DATABASE default SET S3BucketConfig = '[{\"bucket\": \"%s\", \"region\": \"%s\", \"protocol\": \"%s\", \"endpoint\": \"%s\"}]';`,
					c.GetBucket(bucket.Path), bucket.Region, config.GetEndpointProtocol(bucket.Endpoint), c.GetEndpoint(bucket.Endpoint)))

				sb.WriteString(fmt.Sprintf(
					`ALTER DATABASE default SET S3BucketCredentials = '[{\"bucket\": \"%s\", \"accessKey\": \"%s\", \"secretAccessKey\": \"%s\"}]';`,
					c.GetBucket(bucket.Path), accessKey, secretKey))
			}

			if c.Vdb.IsPathHDFS(bucket.Path) {
				if c.Vdb.IsHDFS() {
					continue
				}

				// TODO: set HDFS configuration parameters
			}

			if strings.HasPrefix(bucket.Path, v1.GCloudPrefix) {
				if c.Vdb.IsGCloud() {
					continue
				}

				sb.WriteString(fmt.Sprintf(
					`ALTER DATABASE default SET S3BucketConfig = '[{\"bucket\": \"%s\", \"region\": \"%s\", \"protocol\": \"%s\", \"endpoint\": \"%s\"}]';`,
					c.GetBucket(bucket.Path), bucket.Region, config.GetEndpointProtocol(bucket.Endpoint), c.GetEndpoint(bucket.Endpoint)))

				sb.WriteString(fmt.Sprintf(
					`ALTER DATABASE default SET S3BucketCredentials = '[{\"bucket\": \"%s\", \"accessKey\": \"%s\", \"secretAccessKey\": \"%s\"}]';`,
					c.GetBucket(bucket.Path), accessKey, secretKey))
			}

			if strings.HasPrefix(bucket.Path, v1.AzurePrefix) {
				if c.Vdb.IsAzure() {
					continue
				}

				azureCreds, azureConfig, res, err := c.GetAzureAuth(ctx, bucket.CredentialSecret)
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
	}

	c.Log.Info("executing the following script", "script", sb.String())
	_, _, err := c.PRunner.ExecInPod(ctx, initiatorPod, names.ServerContainer,
		"bash", "-c", cmd,
	)
	c.Log.Info("SQL to be executed after db creation: " + sb.String())
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (c *CreateDBReconciler) generateKubernetesTLSSQL(sb *strings.Builder) {
	fmt.Fprintf(sb, "CREATE OR REPLACE LIBRARY public.KubernetesLib AS ")
	fmt.Fprintf(sb, "'/opt/vertica/packages/kubernetes/lib/libkubernetes.so';\n")
	fmt.Fprintf(sb, "CREATE OR REPLACE SECRETMANAGER KubernetesSecretManager AS LANGUAGE 'C++' ")
	fmt.Fprintf(sb, "NAME 'KubernetesSecretManagerFactory' LIBRARY KubernetesLib;\n")

	fmt.Fprintf(sb, "DROP KEY IF EXISTS https_key_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_cert_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_ca_cert_0;\n")

	fmt.Fprintf(sb, "CREATE KEY https_key_0 TYPE 'rsa' SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		c.Vdb.Spec.NMATLSSecret, corev1.TLSPrivateKeyKey, c.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CA CERTIFICATE https_ca_cert_0 SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}';\n",
		c.Vdb.Spec.NMATLSSecret, paths.HTTPServerCACrtName, c.Vdb.ObjectMeta.Namespace)

	fmt.Fprintf(sb, "CREATE CERTIFICATE https_cert_0 SECRETMANAGER KubernetesSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"data-key\":\"%s\", \"namespace\":\"%s\"}' ",
		c.Vdb.Spec.NMATLSSecret, corev1.TLSCertKey, c.Vdb.ObjectMeta.Namespace)
	fmt.Fprintf(sb, "SIGNED BY https_ca_cert_0 KEY https_key_0;\n")

	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 ADD CA CERTIFICATES ")
	fmt.Fprintf(sb, "https_ca_cert_0 TLSMODE 'TRY_VERIFY';\n")
	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 REMOVE CA CERTIFICATES ")
	fmt.Fprintf(sb, "httpServerRootca;\n")
	fmt.Fprintf(sb, "CREATE AUTHENTICATION k8s_tls_builtin_auth METHOD 'tls' HOST TLS '0.0.0.0/0' FALLTHROUGH;\n")
	fmt.Fprintf(sb, "GRANT AUTHENTICATION k8s_tls_builtin_auth TO %s;\n", c.Vdb.GetVerticaUser())
}

func (c *CreateDBReconciler) generateAWSTlsSQL(sb *strings.Builder) {
	fmt.Fprintf(sb, "CREATE OR REPLACE LIBRARY public.AWSLib AS ")
	fmt.Fprintf(sb, "'/opt/vertica/packages/aws/lib/libaws.so';\n")
	fmt.Fprintf(sb, "CREATE SECRETMANAGER IF NOT EXISTS AWSSecretManager AS ")
	fmt.Fprintf(sb, "LANGUAGE 'C++' NAME 'AWSSecretManagerFactory' LIBRARY AWSLib;\n")

	fmt.Fprintf(sb, "DROP KEY IF EXISTS https_key_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_cert_0;\n")
	fmt.Fprintf(sb, "DROP CERTIFICATE IF EXISTS https_ca_cert_0;\n")

	region, _ := secrets.GetAWSRegion(c.Vdb.Spec.NMATLSSecret)

	secretName := secrets.RemovePathReference(c.Vdb.Spec.NMATLSSecret)
	fmt.Fprintf(sb, "CREATE KEY https_key_0 TYPE 'rsa' SECRETMANAGER AWSSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"json-key\":\"%s\", \"region\":\"%s\"}';\n",
		secretName, corev1.TLSPrivateKeyKey, region)

	fmt.Fprintf(sb, "CREATE CA CERTIFICATE https_ca_cert_0 SECRETMANAGER AWSSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"json-key\":\"%s\", \"region\":\"%s\"}';\n",
		secretName, paths.HTTPServerCACrtName, region)

	fmt.Fprintf(sb, "CREATE CERTIFICATE https_cert_0 SECRETMANAGER AWSSecretManager ")
	fmt.Fprintf(sb, "SECRETNAME '%s' CONFIGURATION '{\"json-key\":\"%s\", \"region\":\"%s\"}' ",
		secretName, corev1.TLSCertKey, region)
	fmt.Fprintf(sb, "SIGNED BY https_ca_cert_0 KEY https_key_0;\n")

	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 ")
	fmt.Fprintf(sb, "ADD CA CERTIFICATES https_ca_cert_0 TLSMODE 'TRY_VERIFY';\n")
	fmt.Fprintf(sb, "ALTER TLS CONFIGURATION https CERTIFICATE https_cert_0 ")
	fmt.Fprintf(sb, "REMOVE CA CERTIFICATES httpServerRootca;\n")
	fmt.Fprintf(sb, "CREATE AUTHENTICATION aws_tls_builtin_auth METHOD 'tls' HOST TLS ")
	fmt.Fprintf(sb, "'0.0.0.0/0' FALLTHROUGH;\n")
	fmt.Fprintf(sb, "GRANT AUTHENTICATION aws_tls_builtin_auth TO %s;\n", c.Vdb.GetVerticaUser())
}

// Escape function to handle special characters in Bash
func escapeForBash(input string) string {
	input = strings.ReplaceAll(input, `"`, `\"`) // Escape double quotes
	return "\"" + input + "\""                   // Wrap in double quotes for echo
}

// postCmdCleanup will handle any cleanup action after initializing the database
func (c *CreateDBReconciler) postCmdCleanup(ctx context.Context) (ctrl.Result, error) {
	pf, ok := c.findPodToRunInit()
	if !ok {
		return ctrl.Result{}, errors.New("could not find a PodFact for create db's post cmd cleanup")
	}
	// The generation of httpstls.json is influenced by the DBInitialized status
	// condition. Now that has changed, we need to set an annotation to continue
	// getting the same behavior. Since the default behavior is to generate the
	// file, we need to set an annotation if we didn't generate the file yet.
	if c.VInf.IsEqualOrNewer(vapi.AutoGenerateHTTPSCertsForNewDatabasesMinVersion) &&
		!pf.GetFileExists()[paths.HTTPTLSConfFileName] {
		chgs := vk8s.MetaChanges{
			NewAnnotations: map[string]string{
				vmeta.HTTPSTLSConfGenerationAnnotation: vmeta.HTTPSTLSConfGenerationAnnotationFalse,
			},
		}
		if _, err := vk8s.MetaUpdate(ctx, c.VRec.Client, c.Vdb.ExtractNamespacedName(), c.Vdb, chgs); err != nil {
			return ctrl.Result{}, err
		}
	}

	// In old versions if encryptSpreadComm was set we need to initiate a restart of the
	// cluster.  If this is needed we do it in a separate reconciler but causing
	// a requeue.
	if c.Vdb.Spec.EncryptSpreadComm != vapi.EncryptSpreadCommDisabled && c.VInf.IsOlder(vapi.SetEncryptSpreadCommAsConfigVersion) {
		c.Log.Info("Requeue reconcile cycle to initiate restart of the server due to encryptSpreadComm setting")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// getPodList gets a list of all of the pods we are going to use with create db.
// If any pod is not found in the pod facts, it return false for the bool
// return value.
func (c *CreateDBReconciler) getPodList() ([]*podfacts.PodFact, bool) {
	// We grab all pods from the first primary subcluster.  Pods for additional
	// subcluster are added through db_add_node.
	sc := c.getFirstPrimarySubcluster()
	podList := make([]*podfacts.PodFact, 0, sc.Size)
	for i := int32(0); i < sc.Size; i++ {
		pn := names.GenPodName(c.Vdb, sc, i)
		pf, ok := c.PFacts.Detail[pn]
		// Bail out if one of the pods in the subcluster isn't found
		if !ok {
			return []*podfacts.PodFact{}, false
		}
		podList = append(podList, pf)
	}
	// We need the podList to be ordered by its compat21 node number. This
	// ensures the assigned vnode number will match the compat21 node number.
	// admintools -t restart_db depends on this.
	sort.Slice(podList, func(i, j int) bool {
		return podList[i].GetCompat21NodeName() < podList[j].GetCompat21NodeName()
	})

	// Check if the shard/node ratio of the first subcluster is good
	c.VRec.checkShardToNodeRatio(c.Vdb, sc)

	// In case that kSafety is 0, we only pick one pod from the first
	// primary subcluster. The remaining pods would be added with db_add_node.
	if c.Vdb.IsKSafety0() {
		return podList[0:1], true
	}
	return podList, true
}

// findPodToRunInit will return a PodFact of the pod that should run the init
// command from
func (c *CreateDBReconciler) findPodToRunInit() (*podfacts.PodFact, bool) {
	// Always return the first pod of the first primary subcluster. We do this
	// so that we can consistently pick the same pod if we have redo the create.
	sc := c.getFirstPrimarySubcluster()
	pf, ok := c.PFacts.Detail[names.GenPodName(c.Vdb, sc, 0)]
	return pf, ok
}

// getFirstPrimarySubcluster returns the first primary subcluster defined in the vdb
func (c *CreateDBReconciler) getFirstPrimarySubcluster() *vapi.Subcluster {
	sc := c.Vdb.GetFirstPrimarySubcluster()
	c.Log.Info("First primary subcluster selected for create_db", "sc", sc.Name)
	return sc
}

// genOptions will return the options to use for the create db command
func (c *CreateDBReconciler) genOptions(ctx context.Context, initiatorPod types.NamespacedName, podNames []types.NamespacedName,
	hostList []string) ([]createdb.Option, error) {
	licPath, err := license.GetPath(ctx, c.VRec.Client, c.Vdb)
	if err != nil {
		return nil, err
	}

	opts := []createdb.Option{
		createdb.WithInitiator(initiatorPod),
		createdb.WithPods(podNames),
		createdb.WithHosts(hostList),
		createdb.WithCatalogPath(c.Vdb.Spec.Local.GetCatalogPath()),
		createdb.WithDBName(c.Vdb.Spec.DBName),
		createdb.WithLicensePath(licPath),
		createdb.WithDepotPath(c.Vdb.Spec.Local.DepotPath),
		createdb.WithDataPath(c.Vdb.Spec.Local.DataPath),
	}

	if !c.VInf.IsEqualOrNewer(vapi.DBSetupConfigParametersMinVersion) {
		opts = append(opts, createdb.WithPostDBCreateSQLFile(PostDBCreateSQLFile))
	}

	// If a communal path is set, include all of the EON parameters.
	if c.Vdb.Spec.Communal.Path != "" {
		opts = append(opts,
			createdb.WithCommunalPath(c.Vdb.GetCommunalPath()),
			createdb.WithCommunalStorageParams(paths.AuthParmsFile),
			createdb.WithConfigurationParams(c.ConfigurationParams.GetMap()),
		)
	}

	if c.Vdb.Spec.ShardCount > 0 {
		opts = append(opts,
			createdb.WithShardCount(c.Vdb.Spec.ShardCount),
		)
	}

	if c.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyCreateSkipPackageInstall {
		if c.VInf.IsEqualOrNewer(vapi.CreateDBSkipPackageInstallVersion) {
			opts = append(opts, createdb.WithSkipPackageInstall())
		}
	}
	return opts, nil
}
