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
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CreateDB will construct a new DB using the vcluster-ops library
func (v *VClusterOps) CreateDB(ctx context.Context, opts ...createdb.Option) (ctrl.Result, error) {
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
		return v.logFailure("VCreateDatabase", events.CreateDBFailed, err)
	}

	v.Log.Info("Successfully created a database", "dbName", vdb.Name)
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genCreateDBOptions(s *createdb.Parms, certs *HTTPSCerts) vops.VCreateDatabaseOptions {
	opts := vops.VCreateDatabaseOptionsFactory()

	opts.RawHosts = s.Hosts
	v.Log.Info("Setup create db options", "hosts", strings.Join(s.Hosts, ","))
	if len(opts.RawHosts) > 0 {
		opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(opts.RawHosts[0]))
	}
	opts.CatalogPrefix = &s.CatalogPath
	opts.DBName = &s.DBName
	opts.LicensePathOnNode = &s.LicensePath
	*opts.ForceRemovalAtCreation = true
	opts.SkipPackageInstall = &s.SkipPackageInstall
	opts.DataPrefix = &s.DataPath

	// If a communal path is set, include all of the EON parameters.
	if s.CommunalPath != "" {
		opts.DepotPrefix = &s.DepotPath
		opts.CommunalStorageLocation = &s.CommunalPath
	}

	// Additional configuration parameters for create db.
	opts.ConfigurationParameters = s.ConfigurationParams

	if s.ShardCount > 0 {
		opts.ShardCount = &s.ShardCount
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = vapi.SuperUser
	if v.Password != "" {
		opts.Password = &v.Password
	}

	return opts
}
