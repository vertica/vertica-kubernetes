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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsc"
)

// StopSubcluster stops the given subcluster
func (v *VClusterOps) StopSubcluster(_ context.Context, opts ...stopsc.Option) error {
	v.setupForAPICall("StopSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster StopSubcluster")

	// get stop subcluster options
	s := stopsc.Parms{}
	s.Make(opts...)

	// call vclusterOps library to stop a subcluster
	vopts := v.genStopSubclusterOptions(&s)
	err := v.VStopSubcluster(&vopts)
	if err != nil {
		_, err = v.logFailure("VStopSubcluster", events.StopSubclusterFailed, err)
		return err
	}

	return nil
}

func (v *VClusterOps) genStopSubclusterOptions(s *stopsc.Parms) vops.VStopSubclusterOptions {
	opts := vops.VStopSubclusterOptionsFactory()

	opts.DBName = v.VDB.Spec.DBName
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.SCName = s.SCName
	// For now we shutdown the subcluster right away
	opts.DrainSeconds = 0
	// auth options
	opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
