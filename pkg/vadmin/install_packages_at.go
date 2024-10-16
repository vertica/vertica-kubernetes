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
	"regexp"
	"strings"

	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

// InstallPackages will install all packages under /opt/vertica/packages where Autoinstall is marked true
func (a *Admintools) InstallPackages(ctx context.Context, opts ...installpackages.Option) (*vops.InstallPackageStatus, error) {
	i := installpackages.Parms{}
	i.Make(opts...)
	cmd := a.genInstallPackagesCmd(&i)
	stdout, err := a.execAdmintools(ctx, i.InitiatorName, cmd...)

	status := genInstallPackageStatus(stdout)
	if err != nil && len(status.Packages) == 0 {
		pkgErr := errors.New(err.Error() + "This may due to lack of memory resources.")
		_, logErr := a.logFailure("install_package", events.InstallPackagesFailed, stdout, pkgErr)
		a.Log.Error(err, "failed to finish package installation", "installPackageStatus", *status)
		return status, logErr
	}
	// If err != nil && len(status.Packages) > 0, we assume it's due to individual package installation failures,
	// rather than fatal errors such as incorrect command line arguments. In line with the
	// behavior of the vclusterops implementation, we don't treat this as an error.
	a.Log.Info("Packages installation finished", "dbName", a.VDB.Spec.DBName,
		"installPackageStatus", *status)
	return status, nil
}

func genInstallPackageStatus(stdout string) *vops.InstallPackageStatus {
	status := &vops.InstallPackageStatus{}
	lines := strings.Split(stdout, "\n")
	if len(lines) > 1 {
		// remove the final empty string after splitting the last "\n"
		lines = lines[:len(lines)-1]
	}
	var currPackageStatus *vops.PackageStatus
	for _, line := range lines {
		if isNewPackage, packageName := isCheckingANewPackage(line); isNewPackage {
			// start processing a new package
			if currPackageStatus != nil {
				// insert the previous package info
				status.Packages = append(status.Packages, *currPackageStatus)
			}
			// initialize a new package status with parsed package name
			currPackageStatus = &vops.PackageStatus{
				PackageName:   packageName,
				InstallStatus: "",
			}
			continue
		}
		// "Installing package {p}..." message should be ignored
		if currPackageStatus != nil && !strings.Contains(line, "Installing package") {
			currPackageStatus.InstallStatus = line
		}
	}
	if currPackageStatus != nil {
		// insert the last package info
		status.Packages = append(status.Packages, *currPackageStatus)
	}
	return status
}

func isCheckingANewPackage(line string) (isNewPackage bool, packageName string) {
	re := regexp.MustCompile(`Checking whether package (.+) is already installed...`)
	match := re.FindStringSubmatch(line)
	if match != nil {
		return true, match[1]
	}
	return false, ""
}

// genInstallPackagesCmd will generate the command line options for calling
// admintools -t install_package.
func (a *Admintools) genInstallPackagesCmd(i *installpackages.Parms) []string {
	cmd := []string{
		"-t", "install_package",
		"--dbname", a.VDB.Spec.DBName,
		"--package", "default",
	}
	if i.ForceReinstall {
		cmd = append(cmd, "--force-reinstall")
	}
	return cmd
}
