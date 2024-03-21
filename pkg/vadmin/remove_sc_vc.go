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
	"errors"

	"github.com/vertica/vcluster/rfc7807"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
)

// RemoveSubcluster will remove the given subcluster from the vertica cluster.
func (v *VClusterOps) RemoveSubcluster(ctx context.Context, opts ...removesc.Option) error {
	v.setupForAPICall("RemoveSubcluster")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster RemoveSubcluster")

	// get the certs
	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return err
	}

	s := removesc.Parms{}
	s.Make(opts...)

	// call vcluster-ops library to remove_subcluster
	vopts := v.genRemoveSubclusterOptions(&s, certs)
	_, err = v.VRemoveSubcluster(&vopts)
	if err != nil {
		vproblem := &rfc7807.VProblem{}
		if ok := errors.As(err, &vproblem); ok {
			if vproblem.Type == rfc7807.SubclusterNotFound.Type {
				// Nothing to do if the subcluster is already gone.
				v.Log.Info("Attempted to remove a subcluster that was already gone", "subcluster", s.Subcluster)
				return nil
			}
		}
	}

	return err
}

func (v *VClusterOps) genRemoveSubclusterOptions(s *removesc.Parms, certs *HTTPSCerts) vops.VRemoveScOptions {
	opts := vops.VRemoveScOptionsFactory()

	// required options
	opts.DBName = &v.VDB.Spec.DBName
	opts.SubclusterToRemove = &s.Subcluster

	opts.RawHosts = []string{s.InitiatorIP}
	v.Log.Info("Setup remove subcluster options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)
	opts.DataPrefix = &v.VDB.Spec.Local.DataPath
	*opts.ForceDelete = true

	if v.VDB.Spec.Communal.Path != "" {
		opts.DepotPrefix = &v.VDB.Spec.Local.DepotPath
	}

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert
	*opts.UserName = v.VDB.GetVerticaUser()
	opts.Password = &v.Password

	return opts
}
