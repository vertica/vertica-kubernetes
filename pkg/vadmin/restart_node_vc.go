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
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RestartNode will restart a subset of nodes. Use this when vertica has not
// lost cluster quorum. The IP given for each vnode may not match the current IP
// in the vertica catalogs.
func (v *VClusterOps) RestartNode(ctx context.Context, opts ...restartnode.Option) (ctrl.Result, error) {
	v.setupForAPICall("StartNode")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster StartNode")

	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	s := restartnode.Parms{}
	s.Make(opts...)

	vcOpts := v.genStartNodeOptions(&s, certs)
	err = v.VStartNodes(vcOpts)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to restart nodes: %w", err)
	}

	return ctrl.Result{}, nil
}

func (v *VClusterOps) genStartNodeOptions(s *restartnode.Parms, certs *HTTPSCerts) *vops.VStartNodesOptions {
	su := v.VDB.GetVerticaUser()
	honorUserInput := true
	opts := vops.VStartNodesOptionsFactory()
	opts.DBName = &v.VDB.Spec.DBName
	opts.RawHosts = []string{s.InitiatorIP}
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP))
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	opts.UserName = &su
	opts.Password = &v.Password
	opts.HonorUserInput = &honorUserInput
	opts.Nodes = s.RestartHosts
	opts.StatePollingTimeout = v.VDB.GetRestartTimeout()
	if v.VDB.IsSideCarDeploymentEnabled() {
		*opts.StartUpConf = paths.StartupConfFile
	}
	return &opts
}
