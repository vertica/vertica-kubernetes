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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/checklicense"
)

func (v *VClusterOps) CheckLicense(ctx context.Context, opts ...checklicense.Option) error {
	v.setupForAPICall("CheckLicense")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster CheckLicense")
	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return err
	}
	s := checklicense.Parms{}
	s.Make(opts...)
	vcOpts := v.genCheckLicenseOptions(&s, certs)
	err = v.VCheckLicense(vcOpts)
	if err != nil {
		return fmt.Errorf("failed to check vertica license: %w", err)
	}
	return nil
}

func (v *VClusterOps) genCheckLicenseOptions(s *checklicense.Parms,
	certs *tls.HTTPSCerts) *vops.VCheckLicenseOptions {
	opts := vops.VCheckLicenseOptionsFactory()

	opts.Hosts = append(opts.Hosts, s.InitiatorIPs...)
	opts.DBName = v.VDB.Spec.DBName

	opts.IsEon = v.VDB.IsEON()
	opts.IPv6 = net.IsIPv6(s.InitiatorIPs[0])

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), v.Password, certs)
	opts.LicenseFile = s.LicenseFile
	opts.CELicenseDisallowed = s.CELicenseDisallowed

	return &opts
}
