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
	"errors"
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// CreateDB will construct a new DB using the vcluster-ops library
func (v *VClusterOps) CreateDB(ctx context.Context, opts ...createdb.Option) (ctrl.Result, error) {
	v.setupForAPICall("CreateDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster CreateDB")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
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
		// If it is determined that vertica is already running, we may allow the
		// failure to continue. This could be a result of a previous failed
		// create db that was able to partially complete but ultimately failed
		// before updating the status condition to indicate that the create db
		// was finished. By ignoring the error this time, we will be able to
		// update the status condition and start any nodes that may be down in
		// order to bring the cluster online.
		dbIsRunningError := &vops.DBIsRunningError{}
		if ok := errors.As(err, &dbIsRunningError); ok {
			failCreateDB := vmeta.FailCreateDBIfVerticaIsRunning(v.VDB.Annotations)
			v.Log.Info("DB create failed because vertica is running", "failCreateDB", failCreateDB)
			if !failCreateDB {
				return ctrl.Result{}, nil
			}
		}
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
	if v.VDB.IsNMASideCarDeploymentEnabled() {
		*opts.StartUpConf = paths.StartupConfFile
	}

	// If a communal path is set, include all of the EON parameters.
	if s.CommunalPath != "" {
		opts.DepotPrefix = &s.DepotPath
		opts.CommunalStorageLocation = &s.CommunalPath
	}

	// Additional configuration parameters for create db.
	opts.ConfigurationParameters = s.ConfigurationParams

	// Flag to generate HTTPS tls conf in vertica bootstrap catalog
	*opts.GenerateHTTPCerts = true

	if s.ShardCount > 0 {
		opts.ShardCount = &s.ShardCount
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = v.VDB.GetVerticaUser()
	if v.Password != "" {
		opts.Password = &v.Password
	}

	return opts
}
