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
)

// ReplicateDB will start replicating data and metadata of an Eon cluster to another
func (v *VClusterOps) ReplicateDB(ctx context.Context, opts ...replicationstart.Option) (int64, error) {
	v.setupForAPICall("ReplicateDB")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster ReplicateDB")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return 0, err
	}

	// get replication start options
	r := replicationstart.Parms{}
	r.Make(opts...)

	// Get target certs
	targetCerts := &HTTPSCerts{}
	if r.Async {
		targetCerts, err = v.retrieveTargetNMACerts(ctx)
		if err != nil {
			return 0, err
		}
	}

	// call vcluster-ops library to replicate db
	vopts := v.genReplicateDBOptions(&r, certs, targetCerts)

	transactionID, err := v.VReplicateDatabase(vopts)
	if err != nil {
		v.Log.Error(err, "failed to replicate a database")
		return 0, err
	}

	if vopts.Async {
		v.Log.Info("Successfully started replication of a database", "sourceDBName", vopts.DBName,
			"targetDBName", vopts.TargetDB, "transactionID", transactionID)
	} else {
		v.Log.Info("Successfully replicated a database", "sourceDBName", vopts.DBName,
			"targetDBName", vopts.TargetDB)
	}
	return transactionID, nil
}

func (v *VClusterOps) genReplicateDBOptions(s *replicationstart.Parms,
	certs *HTTPSCerts, targetCerts *HTTPSCerts) *vops.VReplicationDatabaseOptions {
	opts := vops.VReplicationDatabaseFactory()
	opts.RawHosts = append(opts.RawHosts, s.SourceIP)
	opts.DBName = v.VDB.Spec.DBName
	opts.UserName = s.SourceUserName
	opts.Password = &v.Password
	opts.TargetDB.DBName = s.TargetDBName
	opts.TargetDB.UserName = s.TargetUserName
	opts.SandboxName = s.SourceSandboxName
	if s.SourceTLSConfig != "" {
		opts.TargetDB.Password = nil
	} else {
		opts.TargetDB.Password = &s.TargetPassword
	}
	opts.TargetDB.Hosts = append(opts.TargetDB.Hosts, s.TargetIP)
	opts.SourceTLSConfig = s.SourceTLSConfig
	opts.IsEon = v.VDB.IsEON()

	opts.IPv6 = net.IsIPv6(s.SourceIP)
	opts.TargetDB.IPv6 = net.IsIPv6(s.TargetIP)

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	// Target auth options
	opts.TargetDB.Key = targetCerts.Key
	opts.TargetDB.Cert = targetCerts.Cert
	opts.TargetDB.CaCert = targetCerts.CaCert

	// Async replication options
	opts.Async = s.Async
	opts.TableOrSchemaName = s.ObjectName
	opts.IncludePattern = s.IncludePattern
	opts.ExcludePattern = s.ExcludePattern
	opts.TargetNamespace = s.TargetNamespace

	return &opts
}
