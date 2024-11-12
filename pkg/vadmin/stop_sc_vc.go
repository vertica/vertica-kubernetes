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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsubcluster"
)

// StopSubcluster will stop the subcluster hosts of a running Vertica db
//
//nolint:dupl
func (v *VClusterOps) StopSubcluster(_ context.Context, opts ...stopsubcluster.Option) error {
	v.setupForAPICall("StopSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster StopSubcluster")

	// get stop_sc options
	s := stopsubcluster.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to stop subcluster
	vopts := v.genStopSubclusterOptions(&s)
	err := v.VStopSubcluster(&vopts)
	if err != nil {
		v.Log.Error(err, "failed to stop a subcluster")
		return err
	}

	v.Log.Info("Successfully stopped a subcluster", "scName", vopts.SCName)
	return nil
}

func (v *VClusterOps) genStopSubclusterOptions(s *stopsubcluster.Parms) vops.VStopSubclusterOptions {
	opts := vops.VStopSubclusterOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup stop subcluster options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()

	opts.SCName = s.SCName
	opts.DrainSeconds = s.DrainSeconds
	opts.Force = s.Force

	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
