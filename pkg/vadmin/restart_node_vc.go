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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RestartNode will restart a subset of nodes. Use this when vertica has not
// lost cluster quorum. The IP given for each vnode may not match the current IP
// in the vertica catalogs.
func (v *VClusterOps) RestartNode(ctx context.Context, opts ...restartnode.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster RestartNode")

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	s := restartnode.Parms{}
	s.Make(opts...)

	vcOpts := v.genRestartNodeOptions(&s, certs)
	err = v.VRestartNodes(vcOpts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to restart nodes: %w", err)
	}

	return ctrl.Result{}, nil
}

func (v *VClusterOps) genRestartNodeOptions(s *restartnode.Parms, certs *HTTPSCerts) *vops.VRestartNodesOptions {
	su := vapi.SuperUser
	honorUserInput := true
	opts := vops.VRestartNodesOptions{
		DatabaseOptions: vops.DatabaseOptions{
			DBName:         &v.VDB.Spec.DBName,
			RawHosts:       []string{s.InitiatorIP},
			Ipv6:           vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP)),
			Key:            certs.Key,
			Cert:           certs.Cert,
			CaCert:         certs.CaCert,
			UserName:       &su,
			Password:       &v.Password,
			HonorUserInput: &honorUserInput,
		},
		Nodes: s.RestartHosts,
	}
	// timeout option
	opts.StatePollingTimeout = v.VDB.Spec.RestartTimeout
	return &opts
}
