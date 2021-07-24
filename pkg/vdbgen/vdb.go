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

package vdbgen

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/ioutil"
	"path"
	"regexp"

	// Blank import of vertica since we use it indirectly through the sql interface
	_ "github.com/vertica/vertica-sql-go"
	"k8s.io/apimachinery/pkg/api/resource"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
)

type DBGenerator struct {
	Conn        *sql.DB
	Opts        *Options
	Objs        KObjs
	LicenseData []byte
}

type QueryType string

const (
	ShardCountKey       QueryType = "shardCount"
	CommunalEndpointKey QueryType = "communalEP"
	StorageLocationKey  QueryType = "storageLocation"
	SubclusterQueryKey  QueryType = "subcluster"

	SecretAPIVersion = "v1"
	SecretKindName   = "Secret"
)

var Queries = map[QueryType]string{
	ShardCountKey:       "SELECT COUNT(*) FROM SHARDS WHERE SHARD_TYPE != 'Replica'",
	CommunalEndpointKey: "SHOW DATABASE DEFAULT ALL",
	StorageLocationKey:  "SELECT NODE_NAME, LOCATION_PATH FROM STORAGE_LOCATIONS WHERE LOCATION_USAGE = ?",
	SubclusterQueryKey:  "SELECT SUBCLUSTER_NAME, IS_PRIMARY FROM SUBCLUSTERS ORDER BY NODE_NAME",
}

// Create will generate a VerticaDB based the specifics gathered from a live database
func (d *DBGenerator) Create() (*KObjs, error) {
	ctx := context.Background()
	d.setParmsFromOptions()

	collectors := []func(ctx context.Context) error{
		d.readLicense,
		d.connect,
		d.setShardCount,
		d.setCommunalEndpoint,
		d.setLocalPaths,
		d.setSubclusterDetail,
		d.setCommunalPath,
		d.setLicense,
		d.setPasswordSecret,
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
	d.Objs.Vdb.TypeMeta.APIVersion = vapi.VerticaDBAPIVersion
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

// setCommunalEndpoint will fetch the communal endpoint and set it in v.vdb
func (d *DBGenerator) setCommunalEndpoint(ctx context.Context) error {
	q := Queries[CommunalEndpointKey]
	rows, err := d.Conn.QueryContext(ctx, q)
	if err != nil {
		return fmt.Errorf("failed running '%s': %w", q, err)
	}
	defer rows.Close()

	const HTTPSKey = "AWSEnableHttps"
	const EndpointKey = "AWSEndpoint"
	const AWSAuth = "AWSAuth"
	var protocol, endpoint string
	var auth []string

	for rows.Next() {
		if rows.Err() != nil {
			return fmt.Errorf("failed running '%s': %w", q, rows.Err())
		}
		var key string
		var value string
		if err := rows.Scan(&key, &value); err != nil {
			return fmt.Errorf("failed running '%s': %w", q, err)
		}

		switch key {
		case HTTPSKey:
			if value == "0" {
				protocol = "http"
			} else {
				protocol = "https"
			}
		case EndpointKey:
			endpoint = value

		case AWSAuth:
			authRE := regexp.MustCompile(`:`)
			const NumAuthComponents = 2
			auth = authRE.Split(value, NumAuthComponents)
		}
	}
	if protocol == "" {
		return fmt.Errorf("missing '%s' in query '%s'", HTTPSKey, q)
	}
	if endpoint == "" {
		return fmt.Errorf("missing '%s' in query '%s'", EndpointKey, q)
	}
	if len(auth) == 0 {
		return fmt.Errorf("missing '%s' in query '%s'", AWSAuth, q)
	}

	d.Objs.Vdb.Spec.Communal.Endpoint = fmt.Sprintf("%s://%s", protocol, endpoint)
	d.Objs.CredSecret.Data = map[string][]byte{
		controllers.S3AccessKeyName: []byte(auth[0]),
		controllers.S3SecretKeyName: []byte(auth[1]),
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
	d.Objs.SuperuserPasswordSecret.Data = map[string][]byte{controllers.SuperuserPasswordKey: []byte(d.Opts.Password)}

	return nil
}
