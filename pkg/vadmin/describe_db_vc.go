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

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DescribeDB will get information about a database from communal storage.
func (v *VClusterOps) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	v.Log.Info("Starting vcluster DescribeDB")

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return "", ctrl.Result{}, err
	}

	s := describedb.Parms{}
	s.Make(opts...)

	// We call through to the VReviveDatabase API using a special 'DisplayOnly'
	// option. So, translate the options to revive.
	reviveParms := revivedb.Parms{
		Initiator:             s.Initiator,
		Hosts:                 []string{s.InitiatorIP}, // Only talk to a single host, the initiator
		DBName:                s.DBName,
		CommunalPath:          s.CommunalPath,
		CommunalStorageParams: s.CommunalStorageParams,
		ConfigurationParams:   s.ConfigurationParams,
	}
	vcOpts := v.genReviveDBOptions(&reviveParms, certs)
	*vcOpts.DisplayOnly = true // Set flag to indicate we only want to see the cluster info

	op, err := v.VReviveDatabase(vcOpts)
	if err != nil {
		var res ctrl.Result
		res, err = v.logFailure("VReviveDatabase", events.ReviveDBFailed, err)
		return "", res, err
	}

	return op, ctrl.Result{}, nil
}
