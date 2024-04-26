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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/sandboxsc"
)

// SandboxSubcluster will add a subcluster in a sandbox of the database
func (v *VClusterOps) SandboxSubcluster(_ context.Context, opts ...sandboxsc.Option) error {
	v.setupForAPICall("SandboxSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster SandboxSubcluster")

	// get sandbox_subcluster k8s configs
	s := sandboxsc.Params{}
	s.Make(opts...)

	// call vclusterOps library to sandbox a subcluster
	vopts := v.genSandboxSubclusterOptions(&s)
	err := v.VSandbox(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to add a subcluster to a sandbox", "subcluster", *vopts.SCName, "sandbox", *vopts.SandboxName)
		return err
	}

	v.Log.Info("Successfully added a subcluster to a sandbox", "scName", *vopts.SCName, "sandbox", *vopts.SandboxName)
	return nil
}

func (v *VClusterOps) genSandboxSubclusterOptions(s *sandboxsc.Params) vops.VSandboxOptions {
	opts := vops.VSandboxOptionsFactory()

	opts.DBName = &v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup sandbox subcluster options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SandboxName = &s.Sandbox
	opts.SCName = &s.Subcluster

	// auth options
	*opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
