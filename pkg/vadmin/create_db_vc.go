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

package vadmin

import (
	"context"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CreateDB will construct a new DB using the vcluster-ops library
func (v VClusterOps) CreateDB(ctx context.Context, opts ...createdb.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster CreateDB")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get create_db options
	s := createdb.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to create db
	vopts := v.genCreateDBOptions(&s, certs)
	vdb, err := v.VCreateDatabase(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to create a database")
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully created a database", "dbName", vdb.Name)
	return ctrl.Result{}, nil
}

func (v VClusterOps) genCreateDBOptions(s *createdb.Parms, certs *HTTPSCerts) vops.VCreateDatabaseOptions {
	opts := vops.VCreateDatabaseOptionsFactory()

	opts.RawHosts = s.Hosts
	opts.CatalogPrefix = &s.CatalogPath
	opts.Name = &s.DBName
	opts.LicensePathOnNode = &s.LicensePath
	*opts.ForceRemovalAtCreation = true
	opts.SkipPackageInstall = &s.SkipPackageInstall
	opts.DataPrefix = &s.DataPath

	// If a communal path is set, include all of the EON parameters.
	if s.CommunalPath != "" {
		opts.DepotPrefix = &s.DepotPath
		opts.CommunalStorageLocation = &s.CommunalPath
		// TODO: uncommented this line after vcluster-ops library implemented CommunalStorageParamsPath
		// TODO: might need to create a new NMA endpoint for this option
		// TODO: might need to use paths.AuthParmsFile instead of s.CommunalStorageParams
		// opts.CommunalStorageParamsPath = &s.CommunalStorageParams
	}

	if s.ShardCount > 0 {
		opts.ShardCount = &s.ShardCount
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	// the value of this one needs to be the same as vertica pod's username,
	// otherwise we cannot access vertica HTTPS server
	// TODO: check if we need to add UserName to CRD
	*opts.UserName = "dbadmin"

	// TODO: uncomment this line after vcluster-ops implemented PostDBCreateSQLFile
	// opts.SQLFile = &s.PostDBCreateSQLFile

	// TODO low priority: add this option in vcluster-ops library
	// "`--skip-fs-checks`"

	// TODO low priority: check if we need to add the new options of vcluster-ops create_db to CRD
	// opts.GetAwsCredentialsFromEnv
	// opts.Policy
	// opts.Broadcast
	// opts.P2p
	// opts.LargeCluster
	// opts.Ipv6
	// opts.ClientPort
	// opts.SkipStartupPolling
	// opts.SpreadLogging
	// opts.SpreadLoggingLevel
	// opts.ForceCleanupOnFailure
	// opts.TimeoutNodeStartupSeconds
	// opts.Password
	// opts.ConfigurationParameters

	return opts
}
