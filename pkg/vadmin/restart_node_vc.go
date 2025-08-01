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
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/tls"
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

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	s := restartnode.Parms{}
	s.Make(opts...)

	vcOpts := v.genStartNodeOptions(&s, certs)
	err = v.VStartNodes(vcOpts)
	if err != nil {
		return v.logFailure("VStartNodes", events.NodeRestartFailed, err)
	}
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genStartNodeOptions(s *restartnode.Parms, certs *tls.HTTPSCerts) *vops.VStartNodesOptions {
	opts := vops.VStartNodesOptionsFactory()
	opts.DBName = v.VDB.Spec.DBName
	opts.RawHosts = []string{s.InitiatorIP}
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	opts.Nodes = s.RestartHosts
	vdbTimeout := v.VDB.GetRestartTimeout()
	if vdbTimeout != 0 {
		opts.StatePollingTimeout = vdbTimeout
	}
	if v.VDB.IsNMASideCarDeploymentEnabled() {
		opts.StartUpConf = paths.StartupConfFile
	}
	opts.DoAllowStartUnboundNodes = true
	return &opts
}
