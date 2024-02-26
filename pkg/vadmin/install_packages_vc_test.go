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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/installpackages"
)

const (
	TestPackageName   = "test_package_name"
	TestInstallStatus = "test_installation_status"
)

// mock version of VInstallPackages() that is invoked inside VClusterOps.InstallPackages()
func (m *MockVClusterOps) VInstallPackages(options *vops.VInstallPackagesOptions) (*vops.InstallPackageStatus, error) {
	// verify common options
	err := m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}

	// verify hosts and eon mode
	err = m.VerifyInitiatorIPAndEonMode(&options.DatabaseOptions)
	if err != nil {
		return nil, err
	}

	// verify force reinstall option
	if !*options.ForceReinstall {
		return nil, err
	}

	// verify install packages status
	status := &vops.InstallPackageStatus{
		Packages: []vops.PackageStatus{
			{
				PackageName:   TestPackageName,
				InstallStatus: TestInstallStatus,
			},
		},
	}

	return status, nil
}

var _ = Describe("install_packages_vc", func() {
	ctx := context.Background()

	It("should call vcluster-ops library with install packages task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		status, err := dispatcher.InstallPackages(ctx,
			installpackages.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), TestInitiatorIP),
			installpackages.WithForceReinstall(true),
		)
		Ω(err).Should(Succeed())
		Ω(len(status.Packages)).Should(Equal(1))
		Ω(status.Packages[0].PackageName).Should(Equal(TestPackageName))
		Ω(status.Packages[0].InstallStatus).Should(Equal(TestInstallStatus))
	})
})
