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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstatus"
)

// mock version of VReplicationStatus() that is invoked inside VClusterOps.GetReplicationStatus()
func (m *MockVClusterOps) VReplicationStatus(options *vops.VReplicationStatusDatabaseOptions) (s *vops.ReplicationStatusResponse,
	err error) {
	// verify target db name, target username and target password
	err = m.VerifyTargetDBOptions(&options.TargetDB)
	if err != nil {
		return s, err
	}

	// verify auth options
	err = m.VerifyCerts(&options.TargetDB)
	if err != nil {
		return s, err
	}

	// verify source IP and target IP
	err = m.VerifyTargetIPs(options)
	if err != nil {
		return s, err
	}

	return s, m.VerifyTransactionID(options)
}

var _ = Describe("replication_status_vc", func() {
	ctx := context.Background()

	It("should call GetReplicationStatus in the vcluster-ops library", func() {
		dispatcher := mockVclusteropsDispatcherWithTarget()
		dispatcher.TargetVDB.Spec.DBName = TestDBName
		dispatcher.TargetVDB.Spec.NMATLSSecret = "replication-status-test-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.TargetVDB, dispatcher.Client, dispatcher.TargetVDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		_, err := dispatcher.GetReplicationStatus(ctx,
			replicationstatus.WithTargetIP(TestTargetIP),
			replicationstatus.WithTargetDBName(TestTargetDBName),
			replicationstatus.WithTargetUserName(TestTargetUserName),
			replicationstatus.WithTargetPassword(TestTargetPassword),
			replicationstatus.WithTransactionID(TestTransactionID),
		)
		Ω(err).Should(Succeed())
	})
})
