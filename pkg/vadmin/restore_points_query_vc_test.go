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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restorepoints"
	ctrl "sigs.k8s.io/controller-runtime"
)

// mock version of VShowRestorePoints() that is invoked inside VClusterOps.VShowRestorePoints()
func (m *MockVClusterOps) VShowRestorePoints(options *vops.VShowRestorePointsOptions) (restorePoints []vops.RestorePoint, err error) {
	// verify basic options
	err = m.VerifyDBNameAndIPv6(&options.DatabaseOptions)
	if err != nil {
		return restorePoints, err
	}

	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return restorePoints, err
	}

	if len(options.RawHosts) == 0 || options.RawHosts[0] != TestInitiatorIP {
		return restorePoints, fmt.Errorf("failed to retrieve hosts")
	}

	err = m.VerifyCommunalStorageOptions(*options.CommunalStorageLocation, options.ConfigurationParameters)
	if err != nil {
		return restorePoints, err
	}

	return restorePoints, nil
}

var _ = Describe("restore_points_vc", func() {
	ctx := context.Background()

	var nodeIPs []string
	for i := 0; i < 3; i++ {
		nodeIP := fmt.Sprintf("10.10.10.1%d", i)
		nodeIPs = append(nodeIPs, nodeIP)
	}

	It("should call vcluster-ops library with restore_points task", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "restore-point-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		ctrlRes, err := dispatcher.ShowRestorePoints(ctx,
			restorepoints.WithInitiator(dispatcher.VDB.ExtractNamespacedName(), nodeIPs[0]),
			restorepoints.WithCommunalPath(TestCommunalPath),
			restorepoints.WithConfigurationParams(TestCommunalStorageParams))
		Ω(err).Should(Succeed())
		Ω(ctrlRes).Should(Equal(ctrl.Result{}))
	})
})
