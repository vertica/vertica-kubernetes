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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

// InstallPackages will install all packages under /opt/vertica/packages where Autoinstall is marked true
func (a *Admintools) InstallPackages(ctx context.Context, opts ...installpackages.Option) error {
	s := installpackages.Parms{}
	s.Make(opts...)
	cmd := []string{
		"-t", "install_package",
		"--database", a.VDB.Spec.DBName,
		"--package", "default",
	}
	if s.ForceReinstall {
		cmd = append(cmd, "--force-reinstall")
	}
	_, err := a.execAdmintools(ctx, s.InitiatorName, cmd...)
	return err
}
