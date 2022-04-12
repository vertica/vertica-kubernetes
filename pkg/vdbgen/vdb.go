/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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

package vdbgen

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	// Blank import of vertica since we use it indirectly through the sql interface
	_ "github.com/vertica/vertica-sql-go"
	"k8s.io/apimachinery/pkg/api/resource"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
)

type DBGenerator struct {
	Conn           *sql.DB
	Opts           *Options
	Objs           KObjs
	LicenseData    []byte
	DBCfg          map[string]string // Contents extracted from 'SHOW DATABASE DEFAULT ALL'
	CAFileData     []byte
	HadoopConfData map[string]string
	Krb5ConfData   []byte
	Krb5KeytabData []byte
}

type QueryType string

const (
	ShardCountKey      QueryType = "shardCount"
	DBCfgKey           QueryType = "dbCfg"
	StorageLocationKey QueryType = "storageLocation"
	SubclusterQueryKey QueryType = "subcluster"

	SecretAPIVersion = "v1"
	SecretKindName   = "Secret"

	ConfigAPIVersion = "v1"
	ConfigKindName   = "ConfigMap"
)

var Queries = map[QueryType]string{
	ShardCountKey:      "SELECT COUNT(*) FROM SHARDS WHERE SHARD_TYPE != 'Replica'",
	DBCfgKey:           "SHOW DATABASE DEFAULT ALL",
	StorageLocationKey: "SELECT NODE_NAME, LOCATION_PATH FROM STORAGE_LOCATIONS WHERE LOCATION_USAGE = ?",
	SubclusterQueryKey: "SELECT SUBCLUSTER_NAME, IS_PRIMARY FROM SUBCLUSTERS ORDER BY NODE_NAME",
}

// Create will generate a VerticaDB based the specifics gathered from a live database
func (d *DBGenerator) Create() (*KObjs, error) {
	ctx := context.Background()
	d.setParmsFromOptions()

	collectors := []func(ctx context.Context) error{
		d.readLicense,
		d.connect,
		d.setShardCount,
		d.setCommunalPath,
		d.fetchDatabaseConfig,
		d.setCommunalEndpointAWS,
		d.setCommunalEndpointGCloud,
		d.setCommunalEndpointAzure,
		d.setLocalPaths,
		d.setSubclusterDetail,
		d.setLicense,
		d.setPasswordSecret,
		d.readCAFile,
		d.setCAFile,
		d.readHadoopConfig,
		d.setHadoopConfig,
		d.readKrb5ConfFile,
		d.readKrb5KeytabFile,
		d.setKrb5Secret,
	}

	for _, collector := range collectors {
		if err := collector(ctx); err != nil {
			return nil, err
		}
	}

	return &d.Objs, nil
}

// connect will establish a connect to the database
func (d *DBGenerator) connect(ctx context.Context) error {
	if d.Conn == nil {
		connStr := fmt.Sprintf("vertica://%s:%s@%s:%d/%s?tlsmode=%s",
			d.Opts.User, d.Opts.Password, d.Opts.Host, d.Opts.Port, d.Opts.DBName, d.Opts.TLSMode,
		)
		conn, err := sql.Open("vertica", connStr)
		if err != nil {
			return err
		}
		d.Conn = conn
	}

	return d.Conn.PingContext(ctx)
}

// setParmsFromOptions will set values in the vdb that are obtained from the
// command line options.
func (d *DBGenerator) setParmsFromOptions() {
	d.Objs.Vdb.TypeMeta.APIVersion = vapi.GroupVersion.String()
	d.Objs.Vdb.TypeMeta.Kind = vapi.VerticaDBKind
	d.Objs.Vdb.Spec.InitPolicy = vapi.CommunalInitPolicyRevive
	d.Objs.Vdb.Spec.DBName = d.Opts.DBName
	d.Objs.Vdb.Spec.AutoRestartVertica = true
	d.Objs.Vdb.ObjectMeta.Name = d.Opts.VdbName
	// You cannot omit the RequestSize field.  If you do it shows up as "0", so
	// we need to set the default.
	d.Objs.Vdb.Spec.Local.RequestSize = resource.MustParse("100Mi")

	if d.Opts.IgnoreClusterLease {
		d.Objs.Vdb.Spec.IgnoreClusterLease = true
	}
	if d.Opts.Image != "" {
		d.Objs.Vdb.Spec.Image = d.Opts.Image
	}

	d.Objs.CredSecret.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.CredSecret.TypeMeta.Kind = SecretKindName
	d.Objs.CredSecret.ObjectMeta.Name = fmt.Sprintf("%s-credentials", d.Opts.VdbName)
	d.Objs.Vdb.Spec.Communal.CredentialSecret = d.Objs.CredSecret.ObjectMeta.Name
}

// setShardCount will fetch the shard count from the database and set it inside v.vdb
func (d *DBGenerator) setShardCount(ctx context.Context) error {
	q := Queries[ShardCountKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return fmt.Errorf("failed running '%s': %w", q, rows.Err())
	}
	if !rows.Next() {
		return errors.New("did find any rows in SHARDS")
	}
	if err := rows.Scan(&d.Objs.Vdb.Spec.ShardCount); err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}

	return nil
}

// fetchDatabaseConfig populate the DbCfg with output of the call to
// 'SHOW DATABASE DEFAULT ALL'
func (d *DBGenerator) fetchDatabaseConfig(ctx context.Context) error {
	q := Queries[DBCfgKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	d.DBCfg = map[string]string{}
	for rows.Next() {
		if rows.Err() != nil {
			return fmt.Errorf("failed running '%s': %w", q, rows.Err())
		}
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("failed running '%s': %w", q, err)
		}
		d.DBCfg[key] = value
	}
	return nil
}

// setCommunalEndpointAWS will fetch the communal endpoint for AWS and set it in v.vdb
func (d *DBGenerator) setCommunalEndpointAWS(ctx context.Context) error {
	if !d.Objs.Vdb.IsS3() {
		return nil
	}

	const HTTPSKey = "AWSEnableHttps"
	const EndpointKey = "AWSEndpoint"
	const AWSAuth = "AWSAuth"
	const RegionKey = "AWSRegion"
	return d.setCommunalEndpointGeneric(HTTPSKey, EndpointKey, AWSAuth, RegionKey)
}

// setCommunalEndpointGCloud will fetch the communal endpoint for Google Cloud and set it in v.vdb
func (d *DBGenerator) setCommunalEndpointGCloud(ctx context.Context) error {
	if !d.Objs.Vdb.IsGCloud() {
		return nil
	}

	const HTTPSKey = "GCSEnableHttps"
	const EndpointKey = "GCSEndpoint"
	const AWSAuth = "GCSAuth"
	const RegionKey = "GCSRegion"
	return d.setCommunalEndpointGeneric(HTTPSKey, EndpointKey, AWSAuth, RegionKey)
}

// setCommunalEndpointAzure will look for Azure config and setup the communal
// secret if found.
func (d *DBGenerator) setCommunalEndpointAzure(ctx context.Context) error {
	if !d.Objs.Vdb.IsAzure() {
		return nil
	}

	const AzureCredentialKey = "AzureStorageCredentials"
	const AzureConfigKey = "AzureStorageEndpointConfig"

	credsStr, ok := d.DBCfg[AzureCredentialKey]
	if !ok {
		// Missing entry just means we didn't setup for this endpoint.
		return nil
	}

	cred, ok, err := d.extractAzureCredential(credsStr)
	if !ok || err != nil {
		return err
	}

	// Peek into the azure endpoint config (if it exists), to know if the
	// endpoint is http or https.
	blobEndpoint := cred.BlobEndpoint
	epCfg := cloud.AzureEndpointConfig{}
	configStr, ok := d.DBCfg[AzureConfigKey]
	if ok {
		epCfg, ok, err = d.extractAzureEndpointConfig(configStr)
		if err != nil {
			return err
		}
		if ok && epCfg.Protocol != "" {
			blobEndpoint = fmt.Sprintf("%s://%s", epCfg.Protocol, cred.BlobEndpoint)
		}
	}

	d.Objs.CredSecret.Data = map[string][]byte{}
	if cred.AccountKey != "" {
		d.Objs.CredSecret.Data[cloud.AzureAccountKey] = []byte(cred.AccountKey)
	}
	if cred.AccountName != "" {
		d.Objs.CredSecret.Data[cloud.AzureAccountName] = []byte(cred.AccountName)
	}
	if cred.BlobEndpoint != "" {
		d.Objs.CredSecret.Data[cloud.AzureBlobEndpoint] = []byte(blobEndpoint)
	}
	if cred.SharedAccessSignature != "" {
		d.Objs.CredSecret.Data[cloud.AzureSharedAccessSignature] = []byte(cred.SharedAccessSignature)
	}

	return nil
}

// setCommunalEndpointGeneric gathers information about the endpoint for a
// generic service.  All of the key names are passed in by the caller.
func (d *DBGenerator) setCommunalEndpointGeneric(httpsKey, endpointKey, authKey, regionKey string) error {
	var protocol, endpoint string

	// The db cfg is already loaded in fetchDatabaseConfig
	value, ok := d.DBCfg[httpsKey]
	if !ok {
		// Missing entry just means we didn't setup for this endpoint.
		return nil
	}
	if value == "0" {
		protocol = "http"
	} else {
		protocol = "https"
	}

	value, ok = d.DBCfg[endpointKey]
	if !ok {
		return fmt.Errorf("missing '%s' in query '%s'", endpointKey, Queries[DBCfgKey])
	}
	endpoint = value

	value, ok = d.DBCfg[authKey]
	if !ok {
		return fmt.Errorf("missing '%s' in query '%s'", authKey, Queries[DBCfgKey])
	}
	authRE := regexp.MustCompile(`:`)
	const NumAuthComponents = 2
	auth := authRE.Split(value, NumAuthComponents)

	// The region may not be present if the default was never overridden.
	value, ok = d.DBCfg[regionKey]
	if ok {
		d.Objs.Vdb.Spec.Communal.Region = value
	}

	d.Objs.Vdb.Spec.Communal.Endpoint = fmt.Sprintf("%s://%s", protocol, endpoint)
	d.Objs.CredSecret.Data = map[string][]byte{
		cloud.CommunalAccessKeyName: []byte(auth[0]),
		cloud.CommunalSecretKeyName: []byte(auth[1]),
	}

	return nil
}

// setLocalPaths will fetch the local paths (data and depot) and set it in v.vdb
func (d *DBGenerator) setLocalPaths(ctx context.Context) error {
	dataPath, err := d.queryLocalPath(ctx, "DATA,TEMP")
	if err != nil {
		return err
	}
	d.Objs.Vdb.Spec.Local.DataPath = dataPath

	depotPath, err := d.queryLocalPath(ctx, "DEPOT")
	if err != nil {
		return err
	}
	d.Objs.Vdb.Spec.Local.DepotPath = depotPath

	return nil
}

// setCommunalPath will query the catalog and set the communal path in the vdb.
func (d *DBGenerator) setCommunalPath(ctx context.Context) error {
	var communalPath string
	extractPath := func(nodeName, nodePath sql.NullString) error {
		if !nodePath.Valid {
			return errors.New("node path is NULL")
		}
		communalPath = nodePath.String
		return nil
	}

	if err := d.queryPathForUsage(ctx, "DATA", extractPath); err != nil {
		return err
	}
	d.Objs.Vdb.Spec.Communal.Path = communalPath
	return nil
}

// queryPathForUsage will query the path for a particular usage type.
// This will return the common prefix amongst all nodes.  It will return an
// error if nodes have different paths.
func (d *DBGenerator) queryPathForUsage(ctx context.Context, usage string,
	extractFunc func(nodeName, nodePath sql.NullString) error) error {
	q := Queries[StorageLocationKey]
	rows, err := d.Conn.QueryContext(ctx, q, usage)
	if err != nil {
		return err
	}
	defer rows.Close()

	for rows.Next() {
		if rows.Err() != nil {
			return fmt.Errorf("failed running '%s': %w", q, rows.Err())
		}
		var nodeName sql.NullString
		var nodePath sql.NullString
		if err := rows.Scan(&nodeName, &nodePath); err != nil {
			return fmt.Errorf("failed running '%s': %w", q, err)
		}

		if err := extractFunc(nodeName, nodePath); err != nil {
			return err
		}
	}

	return nil
}

// queryLocalPath will find the local path.  This takes care of multiple nodes
// by extracting out the portion that isn't node specific.  It relies on all
// nodes have the same common prefix.  If they differ an error is returned.
func (d *DBGenerator) queryLocalPath(ctx context.Context, usage string) (string, error) {
	var commonPrefix string

	extractCommonPrefix := func(nodeName, nodePath sql.NullString) error {
		// Extract out the common prefix from the nodePath.  nodePath will be
		// something like /data/vertdb/v_vertdb_node0001_data.  We want to
		// remove the node specific suffix.
		curCommonPrefix := path.Dir(path.Dir(nodePath.String))
		// Check if the prefix matches.  If it doesn't then an error is returned
		// as paths across all nodes must be homogenous.
		if len(commonPrefix) > 0 && commonPrefix != curCommonPrefix {
			return fmt.Errorf(
				"location path for usage '%s' must be the same across all nodes -- path '%s' does not share the common prefix from other nodes '%s'",
				usage, nodePath.String, commonPrefix)
		}
		commonPrefix = curCommonPrefix
		return nil
	}

	if err := d.queryPathForUsage(ctx, usage, extractCommonPrefix); err != nil {
		return "", err
	}

	if commonPrefix == "" {
		return "", fmt.Errorf("failed to find any location path for usage '%s'", usage)
	}

	return commonPrefix, nil
}

// setSubclusterDetail will query the db for details about the subcluster.  This
// will set the subcluster count, size of each and the revive order.
func (d *DBGenerator) setSubclusterDetail(ctx context.Context) error {
	q := Queries[SubclusterQueryKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	// Map to have fast lookup of subcluster name to index in the
	// d.Objs.Vdb.Spec.Subclusters array
	subclusterInxMap := map[string]int{}

	for rows.Next() {
		if rows.Err() != nil {
			return fmt.Errorf("failed running '%s': %w", q, rows.Err())
		}
		var name string
		var isPrimary bool
		if err := rows.Scan(&name, &isPrimary); err != nil {
			return fmt.Errorf("failed running '%s': %w", q, err)
		}

		if !vapi.IsValidSubclusterName(name) {
			return fmt.Errorf("subcluster names are included in the name of statefulsets, but the name "+
				"'%s' cannot be used as it will violate Kubernetes naming.  Please rename the subcluster and "+
				"retry this command again", name)
		}

		inx, ok := subclusterInxMap[name]
		if !ok {
			inx = len(d.Objs.Vdb.Spec.Subclusters)
			// Add an empty subcluster.  We increment the count a few lines down.
			d.Objs.Vdb.Spec.Subclusters = append(d.Objs.Vdb.Spec.Subclusters,
				vapi.Subcluster{Name: name, Size: 0, IsPrimary: isPrimary})
			subclusterInxMap[name] = inx
		}
		d.Objs.Vdb.Spec.Subclusters[inx].Size++

		// Maintain the ReviveOrder.  Update the count of the prior unless the
		// previous node was for a different subcluster.
		revSz := len(d.Objs.Vdb.Spec.ReviveOrder)
		if revSz == 0 || d.Objs.Vdb.Spec.ReviveOrder[revSz-1].SubclusterIndex != inx {
			d.Objs.Vdb.Spec.ReviveOrder = append(d.Objs.Vdb.Spec.ReviveOrder, vapi.SubclusterPodCount{SubclusterIndex: inx, PodCount: 1})
		} else {
			d.Objs.Vdb.Spec.ReviveOrder[revSz-1].PodCount++
		}
	}

	if len(subclusterInxMap) == 0 {
		return errors.New("not subclusters found")
	}
	return nil
}

func (d *DBGenerator) setLicense(ctx context.Context) error {
	// If no license file given, then we omit the license from the manifests
	if len(d.LicenseData) == 0 {
		return nil
	}

	d.Objs.HasLicense = true
	d.Objs.LicenseSecret.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.LicenseSecret.TypeMeta.Kind = SecretKindName
	d.Objs.LicenseSecret.ObjectMeta.Name = fmt.Sprintf("%s-license", d.Opts.VdbName)
	d.Objs.Vdb.Spec.LicenseSecret = d.Objs.LicenseSecret.ObjectMeta.Name
	d.Objs.LicenseSecret.Data = map[string][]byte{"license.dat": d.LicenseData}

	return nil
}

// readLicense reads the license
func (d *DBGenerator) readLicense(ctx context.Context) error {
	// If no license file given, then we omit the license from the manifests
	if d.Opts.LicenseFile == "" {
		return nil
	}

	var err error
	d.LicenseData, err = ioutil.ReadFile(d.Opts.LicenseFile)
	if err != nil {
		return err
	}

	return nil
}

// setPasswordSecret set the password secret fields
func (d *DBGenerator) setPasswordSecret(ctx context.Context) error {
	if d.Opts.Password == "" {
		return nil
	}

	d.Objs.HasPassword = true
	d.Objs.SuperuserPasswordSecret.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.SuperuserPasswordSecret.TypeMeta.Kind = SecretKindName
	d.Objs.SuperuserPasswordSecret.ObjectMeta.Name = fmt.Sprintf("%s-su-passwd", d.Opts.VdbName)
	d.Objs.Vdb.Spec.SuperuserPasswordSecret = d.Objs.SuperuserPasswordSecret.ObjectMeta.Name
	d.Objs.SuperuserPasswordSecret.Data = map[string][]byte{builder.SuperuserPasswordKey: []byte(d.Opts.Password)}

	return nil
}

// readCAFile will read the CA file provided on the command line
func (d *DBGenerator) readCAFile(ctx context.Context) error {
	if d.Opts.CAFile == "" {
		return nil
	}

	var err error
	d.CAFileData, err = ioutil.ReadFile(d.Opts.CAFile)
	return err
}

// setCAFile will capture information about the AWSCAFile and put it into a secret
func (d *DBGenerator) setCAFile(ctx context.Context) error {
	const AWSCAFileKey = "AWSCAFile"
	const SystemCABundlePathKey = "SystemCABundlePath"
	const CACertKey = "ca.crt"

	// The db cfg is already loaded in fetchDatabaseConfig
	_, awsOk := d.DBCfg[AWSCAFileKey]
	_, systemOk := d.DBCfg[SystemCABundlePathKey]
	if !awsOk && !systemOk {
		// Not an error, this just means there is no CA file set
		return nil
	}

	if d.Opts.CAFile == "" {
		return fmt.Errorf("communal endpoint authenticates with a CA file but -cafile not provided")
	}

	d.Objs.HasCAFile = true
	d.Objs.CAFile.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.CAFile.TypeMeta.Kind = SecretKindName
	if d.Opts.CACertName == "" {
		d.Objs.CAFile.ObjectMeta.Name = fmt.Sprintf("%s-ca-cert", d.Opts.VdbName)
	} else {
		d.Objs.CAFile.ObjectMeta.Name = d.Opts.CACertName
	}
	d.Objs.CAFile.Data = map[string][]byte{CACertKey: d.CAFileData}
	d.Objs.Vdb.Spec.CertSecrets = append(d.Objs.Vdb.Spec.CertSecrets,
		vapi.LocalObjectReference{Name: d.Objs.CAFile.ObjectMeta.Name})
	d.Objs.Vdb.Spec.Communal.CaFile = fmt.Sprintf("%s/%s/%s", paths.CertsRoot, d.Objs.CAFile.ObjectMeta.Name, CACertKey)

	return nil
}

// readHadoopConfig will read the contents of the hadoop directory
func (d *DBGenerator) readHadoopConfig(ctx context.Context) error {
	if d.Opts.HadoopConfigDir == "" {
		return nil
	}

	d.HadoopConfData = map[string]string{}

	dir, err := os.Open(d.Opts.HadoopConfigDir)
	if err != nil {
		return err
	}
	defer dir.Close()

	fileNames, err := dir.Readdirnames(0)
	if err != nil {
		return err
	}
	for _, fn := range fileNames {
		if !strings.HasSuffix(fn, ".xml") {
			continue
		}
		cnt, err := ioutil.ReadFile(fmt.Sprintf("%s/%s", d.Opts.HadoopConfigDir, fn))
		if err != nil {
			return err
		}
		d.HadoopConfData[fn] = string(cnt)
	}

	return nil
}

// setHadoopConfig will set the Hadoop config in the Vdb
func (d *DBGenerator) setHadoopConfig(ctx context.Context) error {
	const HadoopConfigKey = "HadoopConfDir"

	_, ok := d.DBCfg[HadoopConfigKey]
	if !ok {
		// Not an error, this just means there is no hadoop conf set
		return nil
	}

	d.Objs.HasHadoopConfig = true
	d.Objs.HadoopConfig.TypeMeta.APIVersion = ConfigAPIVersion
	d.Objs.HadoopConfig.TypeMeta.Kind = ConfigKindName
	d.Objs.HadoopConfig.ObjectMeta.Name = fmt.Sprintf("%s-hadoop-conf", d.Opts.VdbName)
	d.Objs.HadoopConfig.Data = d.HadoopConfData
	d.Objs.Vdb.Spec.Communal.HadoopConfig = d.Objs.HadoopConfig.ObjectMeta.Name

	return nil
}

// extractAzureCredential will grab the Azure credential to be used for communal access.
// nolint:dupl
func (d *DBGenerator) extractAzureCredential(credsStr string) (cloud.AzureCredential, bool, error) {
	// The azure credentials are stored in JSON format.  We should be able to
	// take the string value stored in the database defaults and unmarhsal it
	// into JSON.  If this fails, we will fail with an error.
	creds := []cloud.AzureCredential{}
	if err := json.Unmarshal([]byte(credsStr), &creds); err != nil {
		return cloud.AzureCredential{}, false, fmt.Errorf("unmarshal azure creds: %w", err)
	}

	if len(creds) == 0 {
		// No azure storage credentials setup.  Skip Azure endpoint setup.
		return cloud.AzureCredential{}, false, nil
	}

	// If there is more than one credential stored, then we require the
	// command line option specified that tells what account to use.
	if len(creds) > 1 && d.Opts.AzureAccountName == "" {
		return cloud.AzureCredential{}, false,
			fmt.Errorf("%d azure credentials exist -- must specify the azure account name to use",
				len(creds))
	}

	// We default to the first (and only) credential if no account name was specified.
	if d.Opts.AzureAccountName == "" {
		d.Opts.AzureAccountName = creds[0].AccountName
	}

	cred, ok := d.getAzureCredential(creds)
	if !ok {
		return cloud.AzureCredential{}, false,
			fmt.Errorf("could not find a azure credential with matching account name '%s'",
				d.Opts.AzureAccountName)
	}
	return cred, true, nil
}

// getAzureCredential will find the credential that matches the d.Opts.AzureAccountName
func (d *DBGenerator) getAzureCredential(creds []cloud.AzureCredential) (cloud.AzureCredential, bool) {
	for i := range creds {
		if creds[i].AccountName == d.Opts.AzureAccountName {
			return creds[i], true
		}
	}
	return cloud.AzureCredential{}, false
}

// extractAzureEndpointConfig will parse out the endpoint config for the correct
// accountName.  If nothing is found, the bool return is set to false.
// nolint:dupl
func (d *DBGenerator) extractAzureEndpointConfig(configStr string) (cloud.AzureEndpointConfig, bool, error) {
	// The azure endpoint config is stored in JSON format.  We will be able to
	// take the string value stored in the database defaults and unmarhsal it
	// into JSON.  If this fails, we will fail with an error.
	epCfgs := []cloud.AzureEndpointConfig{}
	if err := json.Unmarshal([]byte(configStr), &epCfgs); err != nil {
		return cloud.AzureEndpointConfig{}, false, fmt.Errorf("unmarshal azure endpoint config: %w", err)
	}

	if len(epCfgs) == 0 {
		// No azure endpoint configs.
		return cloud.AzureEndpointConfig{}, false, nil
	}

	// If there is more than one credential stored, then we require the
	// command line option specified that tells what account to use.
	if len(epCfgs) > 1 && d.Opts.AzureAccountName == "" {
		return cloud.AzureEndpointConfig{}, false,
			fmt.Errorf("%d azure endpoint configs exist -- must specify the azure account name to use",
				len(epCfgs))
	}

	// We default to the first (and only) credential if no account name was specified.
	if d.Opts.AzureAccountName == "" {
		d.Opts.AzureAccountName = epCfgs[0].AccountName
	}

	cfg, ok := d.getAzureConfig(epCfgs)
	if !ok {
		return cloud.AzureEndpointConfig{}, false,
			fmt.Errorf("could not find a azure credential with matching account name '%s'",
				d.Opts.AzureAccountName)
	}
	return cfg, true, nil
}

// getAzureCredential will find the credential that matches the d.Opts.AzureAccountName
func (d *DBGenerator) getAzureConfig(cfgs []cloud.AzureEndpointConfig) (cloud.AzureEndpointConfig, bool) {
	for i := range cfgs {
		if cfgs[i].AccountName == d.Opts.AzureAccountName {
			return cfgs[i], true
		}
	}
	return cloud.AzureEndpointConfig{}, false
}

func (d *DBGenerator) readKrb5ConfFile(ctx context.Context) error {
	if d.Opts.Krb5Conf == "" {
		return nil
	}

	var err error
	d.Krb5ConfData, err = ioutil.ReadFile(d.Opts.Krb5Conf)
	return err
}

func (d *DBGenerator) readKrb5KeytabFile(ctx context.Context) error {
	if d.Opts.Krb5Keytab == "" {
		return nil
	}

	var err error
	d.Krb5KeytabData, err = ioutil.ReadFile(d.Opts.Krb5Keytab)
	return err
}

func (d *DBGenerator) setKrb5Secret(ctx context.Context) error {
	const KerberosServiceNameKey = "KerberosServiceName"
	const KerberosRealmKey = "KerberosRealm"
	realm, okRealm := d.DBCfg[KerberosRealmKey]
	svcName, okSvc := d.DBCfg[KerberosServiceNameKey]

	if !okRealm || !okSvc {
		// Not an error, this just means there is no Kerberos setup
		return nil
	}

	if len(d.Krb5ConfData) == 0 {
		return fmt.Errorf("no krb5.conf data.  Need to specify path to this file with the -krb5conf option")
	}
	if len(d.Krb5KeytabData) == 0 {
		return fmt.Errorf("no krb5.keytab data.  Need to specify path to this file with the -krb5keytab option")
	}

	d.Objs.HasKerberosSecret = true
	d.Objs.Vdb.Spec.Communal.KerberosRealm = realm
	d.Objs.Vdb.Spec.Communal.KerberosServiceName = svcName
	d.Objs.KerberosSecret.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.KerberosSecret.TypeMeta.Kind = SecretKindName
	d.Objs.KerberosSecret.ObjectMeta.Name = fmt.Sprintf("%s-krb5", d.Opts.VdbName)
	d.Objs.KerberosSecret.Data = map[string][]byte{
		filepath.Base(paths.Krb5Conf):   d.Krb5ConfData,
		filepath.Base(paths.Krb5Keytab): d.Krb5KeytabData,
	}
	d.Objs.Vdb.Spec.KerberosSecret = d.Objs.KerberosSecret.ObjectMeta.Name

	return nil
}
