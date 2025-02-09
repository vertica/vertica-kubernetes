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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/renamesc"
)

// RenameSubcluster will rename a subcluster
func (v *VClusterOps) RenameSubcluster(ctx context.Context, opts ...renamesc.Option) error {
	v.setupForAPICall("RenameSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RenameSubcluster")

	// get rename_subcluster k8s configs
	s := renamesc.Params{}
	s.Make(opts...)

	// call vclusterOps library to rename a subcluster
	vopts := v.genRenameSubclusterOptions(&s)
	err := v.VRenameSubcluster(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to rename a subcluster", "subcluster", vopts.SCName, "new subcluster name", vopts.NewSCName)
		return err
	}

	v.Log.Info("Successfully renamed a subcluster", "subcluster", vopts.SCName, "new subcluster name", vopts.NewSCName)
	return nil
}

func (v *VClusterOps) genRenameSubclusterOptions(s *renamesc.Params) vops.VRenameSubclusterOptions {
	opts := vops.VRenameSubclusterFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup rename subcluster options", "hosts", opts.RawHosts)
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SCName = s.Subcluster
	opts.NewSCName = s.NewSubclusterName

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
