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
	"fmt"
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
)

// AddNode will add a new vertica node to the cluster
func (v *VClusterOps) AddNode(ctx context.Context, opts ...addnode.Option) error {
	v.Log.Info("Starting vcluster AddNode")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}

	s := addnode.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to add_node
	vopts := v.genAddNodeOptions(&s, certs)
	vdb, err := v.VAddNode(&vopts)
	if err != nil {
		_, err = v.logFailure("VAddNode", events.AddNodeFailed, err)
		return err
	}
	v.Log.Info(fmt.Sprintf("Successfully added nodes %s to database %s", strings.Join(s.Hosts, ","), vdb.Name))
	return nil
}

func (v *VClusterOps) genAddNodeOptions(s *addnode.Parms, certs *HTTPSCerts) vops.VAddNodeOptions {
	opts := vops.VAddNodeOptionsFactory()

	// required options
	opts.NewHosts = s.Hosts
	opts.DBName = &v.VDB.Spec.DBName

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP))
	opts.SCName = &s.Subcluster
	opts.DataPrefix = &v.VDB.Spec.Local.DataPath
	*opts.HonorUserInput = true
	*opts.ForceRemoval = true
	*opts.SkipRebalanceShards = true
	opts.ExpectedNodeNames = s.ExpectedNodeNames

	if v.VDB.Spec.Communal.Path != "" {
		opts.DepotPrefix = &v.VDB.Spec.Local.DepotPath
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = vapi.SuperUser
	opts.Password = &v.Password

	return opts
}
