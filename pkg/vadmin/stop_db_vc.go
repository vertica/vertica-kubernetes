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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
)

// StopDB will stop all the vertica hosts of a running cluster
func (v *VClusterOps) StopDB(ctx context.Context, opts ...stopdb.Option) error {
	v.Log.Info("Starting vcluster StopDB")

	// get stop_db options
	s := stopdb.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to stop db
	vopts := v.genStopDBOptions(&s)
	err := v.VStopDatabase(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to stop a database")
		return err
	}

	v.Log.Info("Successfully stopped a database", "dbName", *vopts.DBName)
	return nil
}

func (v *VClusterOps) genStopDBOptions(s *stopdb.Parms) vops.VStopDatabaseOptions {
	opts := vops.VStopDatabaseOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup stop db options", "hosts", opts.RawHosts[0])
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP))

	opts.DBName = &v.VDB.Spec.DBName
	opts.IsEon = vstruct.MakeNullableBool(v.VDB.IsEON())

	// auth options
	*opts.UserName = vapi.SuperUser
	opts.Password = &v.Password
	*opts.HonorUserInput = true

	return opts
}
