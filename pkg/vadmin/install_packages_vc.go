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
	"github.com/vertica/vertica-kubernetes/pkg/tls"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

// InstallPackages will install all packages under /opt/vertica/packages where Autoinstall is marked true
func (v *VClusterOps) InstallPackages(ctx context.Context, opts ...installpackages.Option) (*vops.InstallPackageStatus, error) {
	v.setupForAPICall("InstallPackages")
	defer v.tearDownForAPICall()
	v.Log.Info("Starting vcluster InstallPackages")

	// get install_packages options
	s := installpackages.Parms{}
	s.Make(opts...)

	certs, err := v.retrieveHTTPSCerts(ctx)
	if err != nil {
		return &vops.InstallPackageStatus{}, err
	}

	// call vcluster-ops library to install packages
	vopts := v.genInstallPackagesOptions(&s, certs)
	status, err := v.VInstallPackages(&vopts)
	if status == nil {
		status = &vops.InstallPackageStatus{}
	}
	if err != nil {
		_, err = v.logFailure("VInstallPackages", events.InstallPackagesFailed, err)
		v.Log.Error(err, "failed to finish package installation", "installPackageStatus", *status)
		return status, err
	}

	v.Log.Info("Packages installation finished", "dbName", vopts.DBName,
		"installPackageStatus", *status)
	return status, nil
}

func (v *VClusterOps) genInstallPackagesOptions(i *installpackages.Parms, certs *tls.HTTPSCerts) vops.VInstallPackagesOptions {
	opts := vops.VInstallPackagesOptionsFactory()

	opts.RawHosts = append(opts.RawHosts, i.InitiatorIP)
	v.Log.Info("Setup install packages options", "hosts", opts.RawHosts[0])
	opts.IPv6 = net.IsIPv6(i.InitiatorIP)

	opts.DBName = v.VDB.Spec.DBName
	opts.IsEon = v.VDB.IsEON()

	v.setAuthentication(&opts.DatabaseOptions, v.VDB.GetVerticaUser(), &v.Password, certs)

	// force reinstall option
	opts.ForceReinstall = i.ForceReinstall

	return opts
}
