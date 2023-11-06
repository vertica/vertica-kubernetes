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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
)

// RemoveNode will remove an existng vertica node from the cluster.
func (v *VClusterOps) RemoveNode(ctx context.Context, opts ...removenode.Option) error {
	v.setupForAPICall("RemoveNode")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RemoveNode")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}

	s := removenode.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to remove_node
	vopts := v.genRemoveNodeOptions(&s, certs)
	_, err = v.VRemoveNode(&vopts)
	return err
}

func (v *VClusterOps) genRemoveNodeOptions(s *removenode.Parms, certs *HTTPSCerts) vops.VRemoveNodeOptions {
	opts := vops.VRemoveNodeOptionsFactory()

	// required options
	opts.HostsToRemove = s.Hosts
	opts.DBName = &v.VDB.Spec.DBName

	opts.RawHosts = []string{s.InitiatorIP}
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP))
	opts.DataPrefix = &v.VDB.Spec.Local.DataPath
	*opts.HonorUserInput = true

	if v.VDB.Spec.Communal.Path != "" {
		opts.DepotPrefix = &v.VDB.Spec.Local.DepotPath
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = vmeta.GetSuperuserName(v.VDB.Annotations)
	opts.Password = &v.Password

	return opts
}
