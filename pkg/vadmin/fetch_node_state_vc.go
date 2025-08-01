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
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	ctrl "sigs.k8s.io/controller-runtime"
)

// FetchNodeState will determine if the given set of nodes are considered UP
// or DOWN in our consensous state. It returns a map of vnode to its node state.
func (v *VClusterOps) FetchNodeState(ctx context.Context, opts ...fetchnodestate.Option) (map[string]string, ctrl.Result, error) {
	v.setupForAPICall("FetchNodeState")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster FetchNodeState")

	// get fetch node state options
	s := fetchnodestate.Parms{}
	s.Make(opts...)

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return nil, ctrl.Result{}, err
	}

	// call vcluster-ops library to fetch node states
	vopts := v.genFetchNodeStateOptions(&s, certs)
	nodesInfo, err := v.VFetchNodeState(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to fetch node states")
		return nil, ctrl.Result{}, err
	}

	// parse node states
	stateMap := map[string]string{} // node name to state map
	for i := range nodesInfo {
		nodeInfo := &nodesInfo[i]
		stateMap[nodeInfo.Name] = nodeInfo.State
	}

	return stateMap, ctrl.Result{}, nil
}

func (v *VClusterOps) genFetchNodeStateOptions(s *fetchnodestate.Parms, certs *tls.HTTPSCerts) vops.VFetchNodeStateOptions {
	opts := vops.VFetchNodeStateOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)

	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	return opts
}
