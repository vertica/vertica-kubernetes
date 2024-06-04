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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/altersc"
)

const (
	primary   = "primary"
	secondary = "secondary"
)

// AlterSubclusterType will promote/demote a subcluster in the vertica cluster.
func (v *VClusterOps) AlterSubclusterType(ctx context.Context, opts ...altersc.Option) error {
	v.setupForAPICall("AlterSubclusterType")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster AlterSubclusterType")

	// get alter_subcluster_type k8s configs
	s := altersc.Parms{}
	s.Make(opts...)
	if !isValidSCType(s.SCType) {
		return fmt.Errorf("invalid subcluster type: must be %q or %q", primary, secondary)
	}

	// call vcluster-ops library to alter a subcluster
	vopts := v.genAlterSubclusterTypeOptions(&s)
	err := v.VAlterSubclusterType(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to alter a subcluster's type", "scName", vopts.SCName)
		return err
	}
	newType := primary
	if s.SCType == primary {
		newType = secondary
	}

	v.Log.Info("Successfully alter a subcluster's type", "scName", vopts.SCName, "newType", newType)
	return nil
}

func (v *VClusterOps) genAlterSubclusterTypeOptions(s *altersc.Parms) vops.VAlterSubclusterTypeOptions {
	opts := vops.VPromoteDemoteFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SCName = s.SCName
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.SCType = vops.SubclusterType(s.SCType)
	opts.Sandbox = s.Sandbox

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}

func isValidSCType(scType string) bool {
	switch scType {
	case primary, secondary:
		return true
	}
	return false
}
