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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

var _ = Describe("install_package_at", func() {
	ctx := context.Background()

	It("should call admintools -t install_package and parse result", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results[nm] = []cmds.CmdResult{
			{Stdout: "Checking whether package VFunctions is already installed...\n" +
				"Installing package VFunctions...\n" +
				"Failed to install package VFunctions\n" +
				"Checking whether package approximate is already installed...\n" +
				"Installing package approximate...\n" +
				"...Success!\n",
				Err: fmt.Errorf("command terminated with exit code 1"),
			},
		}
		status, err := dispatcher.InstallPackages(ctx,
			installpackages.WithInitiator(nm, "10.9.1.1"),
		)
		Ω(err).Should(Succeed())
		Ω(len(status.Packages)).Should(Equal(2))
		Ω(status.Packages[0].PackageName).Should(Equal("VFunctions"))
		Ω(status.Packages[0].InstallStatus).Should(Equal("Failed to install package VFunctions"))
		Ω(status.Packages[1].PackageName).Should(Equal("approximate"))
		Ω(status.Packages[1].InstallStatus).Should(Equal("...Success!"))
		hist := fpr.FindCommands("-t install_package")
		Ω(len(hist)).Should(Equal(1))
	})
})
