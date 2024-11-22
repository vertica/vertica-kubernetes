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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstatus"
)

// ReplicateDB will start replicating data and metadata of an Eon cluster to another
func (v *VClusterOps) GetReplicationStatus(ctx context.Context, opts ...replicationstatus.Option) (*vops.ReplicationStatusResponse, error) {
	v.setupForAPICall("GetReplicationStatus")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster GetReplicationStatus")

	// Get target certs
	targetCerts, err := v.retrieveTargetNMACerts(ctx)
	if err != nil {
		return nil, err
	}

	// get replication start options
	r := replicationstatus.Parms{}
	r.Make(opts...)

	// call vcluster-ops library to replicate db
	vopts := v.genReplicationStatusOptions(&r, targetCerts)

	status, err := v.VReplicationStatus(vopts)
	if err != nil {
		v.Log.Error(err, "failed to get replication status")
		return nil, err
	}

	v.Log.Info(fmt.Sprintf("Successfully retrieved replication status, %+v", status))

	return status, nil
}

func (v *VClusterOps) genReplicationStatusOptions(s *replicationstatus.Parms,
	targetCerts *HTTPSCerts) *vops.VReplicationStatusDatabaseOptions {
	opts := vops.VReplicationStatusFactory()
	opts.TargetDB.DBName = s.TargetDBName
	opts.TargetDB.UserName = s.TargetUserName
	opts.TargetDB.Password = &s.TargetPassword
	opts.TargetDB.Hosts = append(opts.TargetDB.Hosts, s.TargetIP)
	opts.TargetDB.IPv6 = net.IsIPv6(s.TargetIP)
	opts.TransactionID = s.TransactionID

	// Target auth options
	opts.TargetDB.Key = targetCerts.Key
	opts.TargetDB.Cert = targetCerts.Cert
	opts.TargetDB.CaCert = targetCerts.CaCert

	return &opts
}
