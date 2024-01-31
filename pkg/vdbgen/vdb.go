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

package vdbgen

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	// Blank import of vertica since we use it indirectly through the sql interface
	_ "github.com/vertica/vertica-sql-go"
	"k8s.io/apimachinery/pkg/api/resource"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	vversion "github.com/vertica/vertica-kubernetes/pkg/version"
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
	ShardCountKey          QueryType = "shardCount"
	DBCfgKey               QueryType = "dbCfg"
	StorageLocationKey     QueryType = "storageLocation"
	DiskStorageLocationKey QueryType = "diskStorage"
	NodeCountQueryKey      QueryType = "nodeCount"
	SubclusterQueryKey     QueryType = "subcluster"
	KSafetyQueryKey        QueryType = "ksafety"
	LocalDataSizeQueryKey  QueryType = "storageLocationSize"
	DepotSizeQueryKey      QueryType = "depotSize"
	CatalogSizeQueryKey    QueryType = "catalogSize"
	VersionQueryKey        QueryType = "version"

	SecretAPIVersion = "v1"
	SecretKindName   = "Secret"

	ConfigAPIVersion = "v1"
	ConfigKindName   = "ConfigMap"
)

var Queries = map[QueryType]string{
	ShardCountKey:          "SELECT COUNT(*) FROM SHARDS WHERE SHARD_TYPE != 'Replica'",
	DBCfgKey:               "SHOW DATABASE DEFAULT ALL",
	StorageLocationKey:     "SELECT NODE_NAME, LOCATION_PATH FROM STORAGE_LOCATIONS WHERE LOCATION_USAGE = ?",
	DiskStorageLocationKey: "SELECT NODE_NAME, STORAGE_PATH FROM DISK_STORAGE WHERE STORAGE_USAGE = ?",
	NodeCountQueryKey:      "SELECT COUNT(*) FROM NODES",
	SubclusterQueryKey:     "SELECT SUBCLUSTER_NAME, IS_PRIMARY FROM SUBCLUSTERS ORDER BY NODE_NAME",
	KSafetyQueryKey:        "SELECT GET_DESIGN_KSAFE()",
	DepotSizeQueryKey:      "SELECT MAX(DISK_SPACE_USED_MB+DISK_SPACE_FREE_MB) FROM DISK_STORAGE WHERE STORAGE_USAGE = 'DEPOT'",
	CatalogSizeQueryKey: "SELECT MAX(DISK_SPACE_USED_MB+DISK_SPACE_FREE_MB) " +
		"FROM DISK_STORAGE WHERE STORAGE_USAGE in ('CATALOG','DATA,TEMP')",
	VersionQueryKey: "SELECT VERSION()",
}

// Create will generate a VerticaDB based the specifics gathered from a live database
func (d *DBGenerator) Create() (*KObjs, error) {
	ctx := context.Background()
	d.setParmsFromOptions()

	collectors := []func(ctx context.Context) error{
		d.readLicense,
		d.connect,
		d.setShardCount,
		d.setKSafety,
		d.setImage,
		d.setCommunalPath,
		d.fetchDatabaseConfig,
		d.setCommunalEndpointAWS,
		d.setCommunalEndpointGCloud,
		d.setCommunalEndpointAzure,
		d.setLocalPaths,
		d.setRequestSize,
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
	d.Objs.Vdb.Annotations = make(map[string]string)
	// force deployment method if user specified so
	if d.Opts.DeploymentMethod != "" {
		// only valid options are accepted, thus safe to assign
		if d.Opts.DeploymentMethod == DeploymentMethodAT {
			d.Objs.Vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		} else {
			d.Objs.Vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		}
	}
	d.Objs.Vdb.Annotations[vmeta.SuperuserNameAnnotation] = d.Opts.User
	d.Objs.Vdb.Spec.Communal.AdditionalConfig = make(map[string]string)
	d.Objs.Vdb.Spec.DBName = d.Opts.DBName
	d.Objs.Vdb.Spec.AutoRestartVertica = true
	d.Objs.Vdb.ObjectMeta.Name = d.Opts.VdbName
	// You cannot omit the RequestSize field.  If you do it shows up as "0", so
	// we need to set the default.
	d.Objs.Vdb.Spec.Local.RequestSize = resource.MustParse("100Mi")
	d.Objs.Vdb.Spec.Local.DepotVolume = vapi.DepotVolumeType(d.Opts.DepotVolume)

	if d.Opts.IgnoreClusterLease {
		d.Objs.Vdb.SetIgnoreClusterLease(true)
	}
	if d.Opts.Image != "" {
		d.Objs.Vdb.Spec.Image = d.Opts.Image
	}
}

// setupCredSecret will link a credential secret into the VerticaDB. Use this if
// you know you will populate the credential secret with data.
func (d *DBGenerator) setupCredSecret() {
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

// setKsafety will fetch the ksafety from the database and set it inside v.vdb
func (d *DBGenerator) setKSafety(ctx context.Context) error {
	q := Queries[KSafetyQueryKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return fmt.Errorf("failed running '%s': %w", q, rows.Err())
	}
	if !rows.Next() {
		return errors.New("could not get ksafety from meta-function GET_DESIGN_KSAFE()")
	}
	var designKSafe string
	if err := rows.Scan(&designKSafe); err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	if designKSafe == "0" {
		if nodeCount, err := d.countNodes(ctx); err == nil {
			// vdbgen will fail if ksafety is 0 and there are more than max nodes
			if nodeCount > vapi.KSafety0MaxHosts {
				return fmt.Errorf("VerticaDB does not support ksafety 0 of more than %d nodes", nodeCount)
			}
		} else {
			return err
		}
		d.Objs.Vdb.Annotations[vmeta.KSafetyAnnotation] = "0"
	}
	return nil
}

// countNodes will fetch the number of nodes from the database.
func (d *DBGenerator) countNodes(ctx context.Context) (int, error) {
	q := Queries[NodeCountQueryKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	if rows.Next() {
		if rows.Err() != nil {
			return 0, fmt.Errorf("failed running '%s': %w", q, rows.Err())
		}
		var nodeCount int
		if err := rows.Scan(&nodeCount); err != nil {
			return 0, fmt.Errorf("failed running '%s': %w", q, err)
		}
		return nodeCount, nil
	}

	return 0, nil
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
func (d *DBGenerator) setCommunalEndpointAWS(_ context.Context) error {
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
func (d *DBGenerator) setCommunalEndpointGCloud(_ context.Context) error {
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
func (d *DBGenerator) setCommunalEndpointAzure(_ context.Context) error {
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

	d.setupCredSecret()
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

	value, authFound := d.DBCfg[authKey]
	// The auth may be missing. This can happen if authenticating with IAM
	// profiles.
	if authFound {
		authRE := regexp.MustCompile(`:`)
		const NumAuthComponents = 2
		auth := authRE.Split(value, NumAuthComponents)
		d.setupCredSecret()
		d.Objs.CredSecret.Data = map[string][]byte{
			cloud.CommunalAccessKeyName: []byte(auth[0]),
			cloud.CommunalSecretKeyName: []byte(auth[1]),
		}
	}

	// The region may not be present if the default was never overridden.
	value, ok = d.DBCfg[regionKey]
	if ok {
		d.Objs.Vdb.Spec.Communal.Region = value
	}

	d.Objs.Vdb.Spec.Communal.Endpoint = fmt.Sprintf("%s://%s", protocol, endpoint)

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

	catalogPath, err := d.queryLocalPath(ctx, "CATALOG")
	if err != nil {
		return err
	}
	d.Objs.Vdb.Spec.Local.CatalogPath = catalogPath

	return nil
}

// setRequestSize will fetch the local data size and set it in v.vdb.
func (d *DBGenerator) setRequestSize(ctx context.Context) error {
	depotMaxSize, err := d.queryLocalDataSize(ctx, DepotSizeQueryKey)
	if err != nil {
		return err
	}

	dataMaxSize, err := d.queryLocalDataSize(ctx, CatalogSizeQueryKey)
	if err != nil {
		return err
	}
	requestSize := fmt.Sprintf("%dMi", depotMaxSize+dataMaxSize)
	d.Objs.Vdb.Spec.Local.RequestSize = resource.MustParse(requestSize)

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
	var q string
	const CatalogUsage = "CATALOG"
	// There isn't one table that we can query to get all storage locations. The
	// one for catalog usage is not a true storage location so it doesn't show
	// up in STORAGE_LOCATIONS. Where as communal doesn't show up in
	// DISK_STORAGE. So we have to pick and choose the query depending on the usage.
	if usage != CatalogUsage {
		q = Queries[StorageLocationKey]
	} else {
		q = Queries[DiskStorageLocationKey]
	}
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
		workingDir := nodePath.String
		// When we query the dir for catalog usage, we use a different query
		// that has slightly different output. The table we use puts a /Catalog
		// suffix on the end of the path. We want to take that off before
		// proceeding.
		if usage == "CATALOG" {
			workingDir = path.Dir(workingDir)
		}
		// Extract out the common prefix from the nodePath.  nodePath will be
		// something like /data/vertdb/v_vertdb_node0001_data.  We want to
		// remove the node specific suffix.
		curCommonPrefix := path.Dir(path.Dir(workingDir))
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

// queryLocalDataSize will find data/depot size. It will pick the max among all nodes
func (d *DBGenerator) queryLocalDataSize(ctx context.Context, qtype QueryType) (int64, error) {
	q := Queries[qtype]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return 0, fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return 0, fmt.Errorf("failed running '%s': %w", q, rows.Err())
	}
	if !rows.Next() {
		return 0, errors.New("did not find any rows in DISK_STORAGE")
	}
	var localDataSize int64
	if err := rows.Scan(&localDataSize); err != nil {
		return 0, fmt.Errorf("failed running '%s': %w", q, err)
	}

	return localDataSize, nil
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

		sc := &vapi.Subcluster{
			Name: name,
		}
		if !vapi.IsValidSubclusterName(sc.GenCompatibleFQDN()) {
			return fmt.Errorf("subcluster names are included in the name of statefulsets, but the name "+
				"%q cannot be used as it will violate Kubernetes naming.  Please rename the subcluster so that it matches regex '%s' and "+
				"retry this command again", name, vapi.RFC1123DNSSubdomainNameRegex)
		}

		inx, ok := subclusterInxMap[name]
		var scType string
		if isPrimary {
			scType = vapi.PrimarySubcluster
		} else {
			scType = vapi.SecondarySubcluster
		}
		if !ok {
			inx = len(d.Objs.Vdb.Spec.Subclusters)
			// Add an empty subcluster.  We increment the count a few lines down.
			d.Objs.Vdb.Spec.Subclusters = append(d.Objs.Vdb.Spec.Subclusters,
				vapi.Subcluster{Name: name, Size: 0, Type: scType})
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

// setImage will fetch the server version and use it to pick
// an image that is hosted on our docker repository.
func (d *DBGenerator) setImage(ctx context.Context) error {
	// We just exit if an image was specified on the command line.
	if d.Opts.Image != "" {
		return nil
	}
	q := Queries[VersionQueryKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	if rows.Err() != nil {
		return fmt.Errorf("failed running '%s': %w", q, rows.Err())
	}
	if !rows.Next() {
		return errors.New("could not get Vertica version from meta-function")
	}
	var fullVersion string
	if err = rows.Scan(&fullVersion); err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	// regex to match Vertica version
	exp := regexp.MustCompile(`\d+(?:\.\d+){2}`)
	version := exp.FindString(fullVersion)
	if version == "" {
		return errors.New("could not find Vertica version")
	}
	// Pick an image we hosted in Docker Hub.  We always publish an image for
	// hotfix 0. Rarely do we publish one for subsequent hotfixes, so always use
	// hotfix 0 regardless of what hotfix was currently in use.
	d.Objs.Vdb.Spec.Image = fmt.Sprintf("vertica/vertica-k8s:%s-0", version)

	// Set proper annotation to ensure correct deployment method.
	return d.setDeploymentMethodAnnotationFromServerVersion("v" + version)
}

// setDeploymentMethodAnnotationFromServerVersion will set proper annotation to ensure correct deployment method.
func (d *DBGenerator) setDeploymentMethodAnnotationFromServerVersion(version string) error {
	if _, exists := d.Objs.Vdb.Annotations[vmeta.VClusterOpsAnnotation]; !exists {
		// command line option not provided, i.e. no forced deployment method, thus should
		// determine deployment method based on running server version
		verInfo, err := vversion.MakeInfoFromStrCheck(version)
		if err != nil {
			return err
		}
		if verInfo.IsEqualOrNewer(vapi.VcluseropsAsDefaultDeploymentMethodMinVersion) {
			d.Objs.Vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		} else {
			d.Objs.Vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		}
	}
	return nil
}

func (d *DBGenerator) setLicense(_ context.Context) error {
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
func (d *DBGenerator) readLicense(_ context.Context) error {
	// If no license file given, then we omit the license from the manifests
	if d.Opts.LicenseFile == "" {
		return nil
	}

	var err error
	d.LicenseData, err = os.ReadFile(d.Opts.LicenseFile)
	if err != nil {
		return err
	}

	return nil
}

// setPasswordSecret set the password secret fields
func (d *DBGenerator) setPasswordSecret(_ context.Context) error {
	if d.Opts.Password == "" {
		return nil
	}

	d.Objs.HasPassword = true
	d.Objs.SuperuserPasswordSecret.TypeMeta.APIVersion = SecretAPIVersion
	d.Objs.SuperuserPasswordSecret.TypeMeta.Kind = SecretKindName
	d.Objs.SuperuserPasswordSecret.ObjectMeta.Name = fmt.Sprintf("%s-su-passwd", d.Opts.VdbName)
	d.Objs.Vdb.Spec.PasswordSecret = d.Objs.SuperuserPasswordSecret.ObjectMeta.Name
	d.Objs.SuperuserPasswordSecret.Data = map[string][]byte{builder.SuperuserPasswordKey: []byte(d.Opts.Password)}

	return nil
}

// readCAFile will read the CA file provided on the command line
func (d *DBGenerator) readCAFile(_ context.Context) error {
	if d.Opts.CAFile == "" {
		return nil
	}

	var err error
	d.CAFileData, err = os.ReadFile(d.Opts.CAFile)
	return err
}

// setCAFile will capture information about the AWSCAFile and put it into a secret
func (d *DBGenerator) setCAFile(_ context.Context) error {
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
func (d *DBGenerator) readHadoopConfig(_ context.Context) error {
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
		cnt, err := os.ReadFile(fmt.Sprintf("%s/%s", d.Opts.HadoopConfigDir, fn))
		if err != nil {
			return err
		}
		d.HadoopConfData[fn] = string(cnt)
	}

	return nil
}

// setHadoopConfig will set the Hadoop config in the Vdb
func (d *DBGenerator) setHadoopConfig(_ context.Context) error {
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
	d.Objs.Vdb.Spec.HadoopConfig = d.Objs.HadoopConfig.ObjectMeta.Name

	return nil
}

// extractAzureCredential will grab the Azure credential to be used for communal access.
//
//nolint:dupl
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
//
//nolint:dupl
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

func (d *DBGenerator) readKrb5ConfFile(_ context.Context) error {
	if d.Opts.Krb5Conf == "" {
		return nil
	}

	var err error
	d.Krb5ConfData, err = os.ReadFile(d.Opts.Krb5Conf)
	return err
}

func (d *DBGenerator) readKrb5KeytabFile(_ context.Context) error {
	if d.Opts.Krb5Keytab == "" {
		return nil
	}

	var err error
	d.Krb5KeytabData, err = os.ReadFile(d.Opts.Krb5Keytab)
	return err
}

func (d *DBGenerator) setKrb5Secret(_ context.Context) error {
	realm, okRealm := d.DBCfg[vmeta.KerberosRealmConfig]
	svcName, okSvc := d.DBCfg[vmeta.KerberosServiceNameConfig]

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
	d.Objs.Vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosRealmConfig] = realm
	d.Objs.Vdb.Spec.Communal.AdditionalConfig[vmeta.KerberosServiceNameConfig] = svcName
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
