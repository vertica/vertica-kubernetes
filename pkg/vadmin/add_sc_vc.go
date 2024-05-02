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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
)

// AddSubcluster will create a subcluster in the vertica cluster.
func (v *VClusterOps) AddSubcluster(_ context.Context, opts ...addsc.Option) error {
	v.setupForAPICall("AddSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster AddSubcluster")

	// get add_subcluster k8s configs
	s := addsc.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to add a subcluster
	vopts := v.genAddSubclusterOptions(&s)
	err := v.VAddSubcluster(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to add a subcluster", "scName", vopts.SCName)
		return err
	}

	v.Log.Info("Successfully added a subcluster to the database", "scName", vopts.SCName, "dbName", vopts.DBName)
	return nil
}

func (v *VClusterOps) genAddSubclusterOptions(s *addsc.Parms) vops.VAddSubclusterOptions {
	opts := vops.VAddSubclusterOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup add subcluster options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SCName = s.Subcluster
	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()
	opts.IsPrimary = s.IsPrimary

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
