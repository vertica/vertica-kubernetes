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
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReIP will update the catalog on disk with new IPs for all of the nodes given.
func (v *VClusterOps) ReIP(ctx context.Context, opts ...reip.Option) (ctrl.Result, error) {
	v.setupForAPICall("ReIP")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster ReIP")

	// get the certs
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// get re-ip options
	s := reip.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to re-ip
	vopts := v.genReIPOptions(&s, certs)

	err = v.VReIP(&vopts)
	if err != nil {
		_, err = v.logFailure("VReIP", events.ReipFailed, err)
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully complete re-ip")
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genReIPOptions(s *reip.Parms, certs *HTTPSCerts) vops.VReIPOptions {
	opts := vops.VReIPFactory()

	// hosts
	for _, host := range s.Hosts {
		opts.RawHosts = append(opts.RawHosts, host.IP)
	}
	v.Log.Info("Setup re-ip options", "hosts", strings.Join(opts.RawHosts, ","))

	// ipv6
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(opts.RawHosts[0]))

	// catalog prefix
	*opts.CatalogPrefix = v.VDB.Spec.Local.GetCatalogPath()

	// database name
	opts.DBName = &v.VDB.Spec.DBName

	// re-ip list
	for _, h := range s.Hosts {
		var reIPInfo vops.ReIPInfo
		reIPInfo.NodeName = h.VNode
		reIPInfo.TargetAddress = h.IP
		opts.ReIPList = append(opts.ReIPList, reIPInfo)
	}

	// eon options
	// Provide eon options to vclusterops only after revive_db because
	// we do not need to access communal storage in re_ip after create_db.
	if v.VDB.Spec.InitPolicy == vapi.CommunalInitPolicyRevive {
		opts.IsEon = vstruct.MakeNullableBool(v.VDB.IsEON())
		*opts.CommunalStorageLocation = s.CommunalPath
		opts.ConfigurationParameters = s.ConfigurationParams
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = vapi.SuperUser
	opts.Password = &v.Password
	*opts.HonorUserInput = true

	return opts
}
