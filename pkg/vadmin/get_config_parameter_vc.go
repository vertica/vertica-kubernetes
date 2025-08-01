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
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/getconfigparameter"
)

// GetConfigurationParameter can get the value of a given configuration parameter
func (v *VClusterOps) GetConfigurationParameter(ctx context.Context, opts ...getconfigparameter.Option) (string, error) {
	v.setupForAPICall("GetConfigurationParameter")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster GetConfigurationParameter")

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return "", err
	}

	s := getconfigparameter.Params{}
	s.Make(opts...)

	vcOpts := v.genGetConfigurationParameterOptions(&s, certs)
	value, err := v.VGetConfigurationParameters(vcOpts)
	if err != nil {
		return "", fmt.Errorf("failed to get configuration parameter: %w", err)
	}

	return value, nil
}

func (v *VClusterOps) genGetConfigurationParameterOptions(s *getconfigparameter.Params,
	certs *tls.HTTPSCerts) *vops.VGetConfigurationParameterOptions {
	opts := vops.VGetConfigurationParameterOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	opts.DBName = v.VDB.Spec.DBName

	opts.Sandbox = s.Sandbox
	opts.ConfigParameter = s.ConfigParameter
	opts.Level = s.Level

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	opts.UserName = v.VDB.GetVerticaUser()
	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	return &opts
}
