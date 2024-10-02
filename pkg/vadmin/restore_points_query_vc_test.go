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
	vops "github.com/vertica/vcluster/vclusterops"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
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

	err = m.VerifyCommunalStorageOptions(options.CommunalStorageLocation, options.ConfigurationParameters)
	if err != nil {
		return restorePoints, err
	}

	err = m.VerifyFilterOptions(&options.FilterOptions)
	if err != nil {
		return restorePoints, err
	}

	// Dummy restore points data for testing
	restorePoints = []vops.RestorePoint{
		{Archive: "db", Timestamp: "2024-02-06 07:25:07.437957", ID: "1465516c-e207-4d33-ae62-ce7cd5cfe8d0", Index: 1},
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

	It("should call ShowRestorePoints in the vcluster-ops library", func() {
		dispatcher := mockVClusterOpsDispatcher()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "restore-point-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		showRestorePoints, err := dispatcher.ShowRestorePoints(ctx,
			showrestorepoints.WithInitiator(nodeIPs[0]),
			showrestorepoints.WithCommunalPath(TestCommunalPath),
			showrestorepoints.WithConfigurationParams(TestCommunalStorageParams),
			showrestorepoints.WithArchiveNameFilter(TestArchiveName),
			showrestorepoints.WithStartTimestampFilter(TestStartTimestamp),
			showrestorepoints.WithEndTimestampFilter(TestEndTimestamp),
		)
		Ω(err).Should(Succeed())
		Ω(len(showRestorePoints)).Should(Equal(1))
		Ω(showRestorePoints[0].Archive).Should(Equal("db"))
		Ω(showRestorePoints[0].Index).Should(Equal(1))
	})
})
