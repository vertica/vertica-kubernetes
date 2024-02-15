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

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vcluster/vclusterops/vstruct"
	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
)

// ShowRestorePoints can query the restore points from an archive. It can
// show list restore points in a database
func (v *VClusterOps) ShowRestorePoints(ctx context.Context, opts ...showrestorepoints.Option) (restorePoints []vops.RestorePoint,
	err error) {
	v.setupForAPICall("ShowRestorePoints")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster ShowRestorePoints")

	certs, err := v.retrieveNMACerts(ctx)
	if err != nil {
		return restorePoints, err
	}

	s := showrestorepoints.Parms{}
	s.Make(opts...)

	vcOpts := v.genRestorePointsOptions(&s, certs)
	showRestorePoints, err := v.VShowRestorePoints(vcOpts)
	if err != nil {
		return showRestorePoints, fmt.Errorf("failed to show restore points: %w", err)
	}

	return showRestorePoints, nil
}

func (v *VClusterOps) genRestorePointsOptions(s *showrestorepoints.Parms, certs *HTTPSCerts) *vops.VShowRestorePointsOptions {
	opts := vops.VShowRestorePointsFactory()

	// required options
	opts.DBName = &v.VDB.Spec.DBName
	opts.CommunalStorageLocation = &s.CommunalPath

	*opts.HonorUserInput = true
	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	v.Log.Info("Setup restore point options", "rawhosts", opts.RawHosts)

	opts.Ipv6 = vstruct.MakeNullableBool(net.IsIPv6(s.InitiatorIP))
	opts.ConfigurationParameters = s.ConfigurationParams

	// auth options
	opts.Key = certs.Key
	opts.Cert = certs.Cert
	opts.CaCert = certs.CaCert

	// optional query filter options
	opts.FilterOptions = &s.FilterOptions

	return &opts
}
