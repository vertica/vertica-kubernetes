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
	"github.com/vertica/vcluster/vclusterops/vstruct"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReviveDB will initialized a database using an existing communal path. It does
// this using the vclusterops library.
func (v *VClusterOps) ReviveDB(ctx context.Context, opts ...revivedb.Option) (ctrl.Result, error) {
	v.setupForAPICall("ReviveDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting VCluster ReviveDB")

	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	s := revivedb.Parms{}
	s.Make(opts...)

	vcOpts := v.genReviveDBOptions(&s, certs)
	_, _, err = v.VReviveDatabase(vcOpts)
	if err != nil {
		return v.logFailure("VReviveDatabase", events.ReviveDBFailed, err)
	}

	return ctrl.Result{}, nil
}

func (v *VClusterOps) genReviveDBOptions(s *revivedb.Parms, certs *HTTPSCerts) *vops.VReviveDatabaseOptions {
	opts := vops.VReviveDBOptionsFactory()

	opts.DBName = &v.VDB.Spec.DBName
	opts.RawHosts = s.Hosts
	v.Log.Info("Setup revive database options", "hosts", opts.RawHosts)
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(opts.RawHosts[0]))
	opts.CommunalStorageLocation = &s.CommunalPath
	opts.ConfigurationParameters = s.ConfigurationParams
	*opts.ForceRemoval = true
	*opts.IgnoreClusterLease = s.IgnoreClusterLease

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
