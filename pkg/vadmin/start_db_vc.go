// /*
//  (c) Copyright [2021-2023] Open Text.
//  Licensed under the Apache License, Version 2.0 (the "License");
//  You may not use this file except in compliance with the License.
//  You may obtain a copy of the License at

//  http://www.apache.org/licenses/LICENSE-2.0

//  Unless required by applicable law or agreed to in writing, software
//  distributed under the License is distributed on an "AS IS" BASIS,
//  WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
//  See the License for the specific language governing permissions and
//  limitations under the License.
// */

package vadmin

import (
	"context"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartDB will start a subset of nodes of the database
func (v *VClusterOps) StartDB(ctx context.Context, opts ...startdb.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster StartDB")

	// get start_db options
	s := startdb.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to start db
	vopts := v.genStartDBOptions(&s)
	err := v.VStartDatabase(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to start a database")
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully start a database", "dbName", *vopts.Name)
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genStartDBOptions(s *startdb.Parms) vops.VStartDatabaseOptions {
	opts := vops.VStartDatabaseOptionsFactory()
	opts.RawHosts = s.Hosts
	if len(opts.RawHosts) > 0 {
		opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(opts.RawHosts[0]))
	}
	*opts.CatalogPrefix = v.VDB.Spec.Local.GetCatalogPath()
	opts.Name = &v.VDB.Spec.DBName
	opts.IsEon = vstruct.MakeNullableBool(v.VDB.IsEON())

	// auth options
	*opts.UserName = vapi.SuperUser
	opts.Password = &v.Password
	*opts.HonorUserInput = true
	return opts
}
