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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/promotesandboxtomain"
)

// ShowRestorePoints can query the restore points from an archive. It can
// show list restore points in a database
func (v *VClusterOps) PromoteSandboxToMain(ctx context.Context, opts ...promotesandboxtomain.Option) (err error) {
	v.setupForAPICall("PromoteSandboxToMain")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster PromoteSandboxToMain")

	s := promotesandboxtomain.Params{}
	s.Make(opts...)

	vcOpts := v.genPromoteSandboxToMainOptions(&s)
	err = v.VPromoteSandboxToMain(vcOpts)
	if err != nil {
		return fmt.Errorf("failed to promote sandbox to main: %w", err)
	}

	return nil
}

func (v *VClusterOps) genPromoteSandboxToMainOptions(s *promotesandboxtomain.Params) *vops.VPromoteSandboxToMainOptions {
	opts := vops.VPromoteSandboxToMainFactory()

	// required options
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup promote sandbox to main options", "hosts", opts.RawHosts)
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SandboxName = s.Sandbox

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return &opts
}
