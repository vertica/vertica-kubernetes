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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

// InstallPackages will install all packages under /opt/vertica/packages where Autoinstall is marked true
func (a *Admintools) InstallPackages(ctx context.Context, opts ...installpackages.Option) error {
	i := installpackages.Parms{}
	i.Make(opts...)
	cmd := a.genInstallPackagesCmd(&i)
	_, err := a.execAdmintools(ctx, i.InitiatorName, cmd...)
	return err
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
