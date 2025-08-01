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

package vadmin

import (
	"context"
	"fmt"
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartDB will start a subset of nodes of the database
func (v *VClusterOps) StartDB(ctx context.Context, opts ...startdb.Option) (ctrl.Result, error) {
	v.setupForAPICall("StartDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster StartDB")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get start_db options
	s := startdb.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to start db
	vopts, err := v.genStartDBOptions(&s, certs)
	if err != nil {
		v.Log.Error(err, "failed to set up start db options")
		return ctrl.Result{}, err
	}

	_, err = v.VStartDatabase(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to start a database")
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully start a database", "dbName", vopts.DBName)
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genStartDBOptions(s *startdb.Parms, certs *tls.HTTPSCerts) (vops.VStartDatabaseOptions, error) {
	opts := vops.VStartDatabaseOptionsFactory()
	opts.RawHosts = s.Hosts
	v.Log.Info("Setup start db options", "hosts", strings.Join(s.Hosts, ","))
	if len(opts.RawHosts) == 0 {
		return vops.VStartDatabaseOptions{}, fmt.Errorf("hosts should not be empty %s", opts.RawHosts)
	}
	opts.IPv6 = net.IsIPv6(opts.RawHosts[0])
	opts.CatalogPrefix = v.VDB.Spec.Local.GetCatalogPath()
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.ConfigurationParameters = s.ConfigurationParams
	opts.HostsInSandbox = s.HostsInSandbox
	if v.VDB.IsNMASideCarDeploymentEnabled() {
		opts.StartUpConf = paths.StartupConfFile
	}

	// Provide communal storage location to vclusterops only after revive_db because
	// we do not need to access communal storage in start_db after create_db.
	if v.VDB.Spec.InitPolicy == vapi.CommunalInitPolicyRevive {
		opts.CommunalStorageLocation = s.CommunalPath
	}

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	// timeout option
	vdbTimeout := v.VDB.GetRestartTimeout()
	if vdbTimeout != 0 {
		opts.StatePollingTimeout = vdbTimeout
	}

	// other options
	opts.TrimHostList = true

	return opts, nil
}
