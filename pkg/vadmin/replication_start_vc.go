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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReplicateDB will start replicating data and metadata of an Eon cluster to another
func (v *VClusterOps) ReplicateDB(ctx context.Context, opts ...replicationstart.Option) (ctrl.Result, error) {
	v.setupForAPICall("ReplicateDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster ReplicateDB")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get replication start options
	r := replicationstart.Parms{}
	r.Make(opts...)

	// call vcluster-ops library to replicate db
	vopts := v.genReplicateDBOptions(&r, certs)

	err = v.VReplicateDatabase(vopts)
	if err != nil {
		v.Log.Error(err, "failed to replicate a database")
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully replicated a database", "sourceDBName", vopts.DBName,
		"targetDBName", vopts.TargetDB)
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genReplicateDBOptions(s *replicationstart.Parms, certs *HTTPSCerts) *vops.VReplicationDatabaseOptions {
	opts := vops.VReplicationDatabaseFactory()
	opts.RawHosts = append(opts.RawHosts, s.SourceIP)
	opts.DBName = v.VDB.Spec.DBName
	opts.UserName = s.SourceUserName
	opts.Password = &v.Password
	opts.TargetDB = s.TargetDBName
	opts.TargetUserName = s.TargetUserName
	if s.SourceTLSConfig != "" {
		opts.TargetPassword = nil
	} else {
		opts.TargetPassword = &s.TargetPassword
	}
	opts.TargetHosts = append(opts.TargetHosts, s.TargetIP)
	opts.SourceTLSConfig = s.SourceTLSConfig
	opts.IsEon = v.VDB.IsEON()

	opts.IPv6 = net.IsIPv6(s.SourceIP)

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	return &opts
}
