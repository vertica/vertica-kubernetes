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
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReIP will update the catalog on disk with new IPs for all of the nodes given.
func (v *VClusterOps) ReIP(ctx context.Context, opts ...reip.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster ReIP")

	// get re-ip options
	s := reip.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to re-ip
	vopts, err := v.genReIPOptions(&s)
	if err != nil {
		v.Log.Error(err, "failed to set up re-ip options")
		return ctrl.Result{}, err
	}

	err = v.VReIP(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to run re-ip")
		return ctrl.Result{}, err
	}

	v.Log.Info("Successfully complete re-ip")
	return ctrl.Result{}, nil
}

func (v *VClusterOps) genReIPOptions(s *reip.Parms) (vops.VReIPOptions, error) {
	opts := vops.VReIPFactory()

	// hosts
	for _, host := range s.Hosts {
		opts.Hosts = append(opts.Hosts, host.IP)
	}
	v.Log.Info("Setup re-ip options", "hosts", strings.Join(opts.Hosts, ","))
	if len(opts.Hosts) == 0 {
		return vops.VReIPOptions{}, fmt.Errorf("hosts should not be empty")
	}

	// ipv6
	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(opts.Hosts[0]))

	// catalog prefix
	*opts.CatalogPrefix = v.VDB.Spec.Local.GetCatalogPath()

	// database name
	opts.Name = &v.VDB.Spec.DBName

	// auth options
	*opts.UserName = vapi.SuperUser
	opts.Password = &v.Password
	*opts.HonorUserInput = true
	return opts, nil
}
