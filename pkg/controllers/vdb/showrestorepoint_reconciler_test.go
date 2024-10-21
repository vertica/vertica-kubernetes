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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("showrestorepoint_reconciler", func() {
	ctx := context.Background()
	const archive = "arch"

	It("should exit early", func() {
		vdb := vapi.MakeVDB()
		vdb.Status = vapi.VerticaDBStatus{}
		// should exit because status.restorePoint is nil
		Expect(shouldEarlyExit(vdb)).Should(BeTrue())
		vdb.Status.RestorePoint = &vapi.RestorePointInfo{
			Archive:        archive,
			StartTimestamp: "start",
		}
		// should exit because at least one of archive, startTimeStamp,
		// endTimeStamp is not set
		Expect(shouldEarlyExit(vdb)).Should(BeTrue())
		vdb.Status.RestorePoint.EndTimestamp = "end"
		// should not exit
		Expect(shouldEarlyExit(vdb)).Should(BeFalse())
		vdb.Status.RestorePoint.Details = &vclusterops.RestorePoint{}
		// should exit because status.restorePoint.details is already set
		Expect(shouldEarlyExit(vdb)).Should(BeTrue())
	})

	It("should update status with restore point info", func() {
		vdb := vapi.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		s := &ShowRestorePointReconciler{
			Vdb:  vdb,
			VRec: vdbRec,
			Log:  logger,
		}
		const id = "abcdef"
		rpts := []vclusterops.RestorePoint{
			{
				Archive: archive,
				ID:      id,
				Index:   1,
			},
		}
		Expect(s.saveRestorePointDetailsInVDB(ctx, rpts)).Should(Succeed())
		fetchedVdb := &vapi.VerticaDB{}
		Expect(k8sClient.Get(ctx, vdb.ExtractNamespacedName(), fetchedVdb)).Should(Succeed())
		Expect(fetchedVdb.Status.RestorePoint.Details.Archive).Should(Equal(archive))
		Expect(fetchedVdb.Status.RestorePoint.Details.ID).Should(Equal(id))
		Expect(fetchedVdb.Status.RestorePoint.Details.Index).Should(Equal(1))
	})
})
