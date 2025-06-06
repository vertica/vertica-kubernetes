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
	"maps"

	vops "github.com/vertica/vcluster/vclusterops"

	"github.com/vertica/vertica-kubernetes/pkg/net"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/settlsconfig"
)

// SetTLSConfig given an https and client server secret, will set tls configuration
// in the database.
//
//nolint:dupl
func (v *VClusterOps) SetTLSConfig(ctx context.Context, opts ...settlsconfig.Option) error {
	v.setupForAPICall("SetTLSConfig")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster SetTLSConfig")

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}

	s := settlsconfig.Parms{}
	s.Make(opts...)

	vcOpts := v.genSetTLSConfigOptions(&s, certs)
	err = v.VSetTLSConfig(vcOpts)
	if err != nil {
		return fmt.Errorf("failed to set tls config: %w", err)
	}

	return nil
}

func (v *VClusterOps) genSetTLSConfigOptions(s *settlsconfig.Parms,
	certs *HTTPSCerts) *vops.VSetTLSConfigOptions {
	opts := vops.VSetTLSConfigOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, s.InitiatorIP)
	opts.DBName = v.VDB.Spec.DBName

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIP)

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)
	if v.Password != "" {
		opts.Password = &v.Password
	}
	if s.IsHTTPSTLSConfig {
		configMap := genTLSConfigurationMap(s.HTTPSTLSMode, s.HTTPSTLSSecretName, s.Namespace)
		opts.HTTPSTLSConfig.SetConfigMap(maps.Clone(configMap))
		opts.HTTPSTLSConfig.GrantAuth = s.GrantAuth
		opts.ServerTLSConfig.GrantAuth = !s.GrantAuth
	} else {
		configMap := genTLSConfigurationMap(s.ClientServerTLSMode, s.ClientServerTLSSecretName, s.Namespace)
		opts.ServerTLSConfig.SetConfigMap(maps.Clone(configMap))
		opts.ServerTLSConfig.GrantAuth = s.GrantAuth
		opts.HTTPSTLSConfig.GrantAuth = !s.GrantAuth
	}

	return &opts
}
