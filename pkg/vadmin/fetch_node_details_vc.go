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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodedetails"
)

// FetchNodeDetails will return details for a node, including its state, sandbox, and storage locations
func (v *VClusterOps) FetchNodeDetails(_ context.Context, opts ...fetchnodedetails.Option) (vops.NodeDetails, error) {
	v.setupForAPICall("FetchNodeDetails")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster FetchNodeDetails")

	// get fetch node details options
	s := fetchnodedetails.Parms{}
	s.Make(opts...)

	// call vclusterOps library to fetch node details
	vopts := v.genFetchNodeDetailsOptions(&s)
	nodesDetails, err := v.VFetchNodesDetails(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to fetch node details")
		return vops.NodeDetails{}, err
	}

	// parse node details
	if len(nodesDetails) != 1 {
		err := fmt.Errorf("expected details for one node, but received details for %d", len(nodesDetails))
		v.Log.Error(err, "failed to process node details")
		return vops.NodeDetails{}, err
	}

	return nodesDetails[0], nil
}

func (v *VClusterOps) genFetchNodeDetailsOptions(s *fetchnodedetails.Parms) vops.VFetchNodesDetailsOptions {
	opts := vops.VFetchNodesDetailsOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)

	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
