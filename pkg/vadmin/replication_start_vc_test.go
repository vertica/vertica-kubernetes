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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/replicationstart"
)

// mock version of VReplicateDatabase() that is invoked inside VClusterOps.ReplicateDB()
func (m *MockVClusterOps) VReplicateDatabase(options *vops.VReplicationDatabaseOptions) (i int64, err error) {
	// verify source db name, source username and source password
	err = m.VerifyCommonOptions(&options.DatabaseOptions)
	if err != nil {
		return i, err
	}

	// verify target db name, target username and target password
	err = m.VerifyTargetDBReplicationOptions(options)
	if err != nil {
		return i, err
	}

	// verify auth options
	err = m.VerifyCerts(&options.DatabaseOptions)
	if err != nil {
		return i, err
	}

	// verify target DB auth options
	err = m.VerifyCerts(&options.TargetDB)
	if err != nil {
		return i, err
	}

	// verify source IP and target IP
	err = m.VerifySourceAndTargetIPs(options)
	if err != nil {
		return i, err
	}

	// verify source TLS config
	err = m.VerifySourceTLSConfig(options)
	if err != nil {
		return i, err
	}

	// verify eon mode
	err = m.VerifyEonMode(&options.DatabaseOptions)
	if err != nil {
		return i, err
	}

	// verify target db name, target username and target password
	return i, m.VerifyAsyncReplicationOptions(options)
}

var _ = Describe("replication_start_vc", func() {
	ctx := context.Background()

	It("should call ReplicateDB in the vcluster-ops library", func() {
		dispatcher := mockVclusteropsDispatcherWithTarget()
		dispatcher.VDB.Spec.DBName = TestDBName
		dispatcher.VDB.Spec.NMATLSSecret = "replication-start-test-secret"
		dispatcher.TargetVDB.Spec.DBName = TestTargetDBName
		dispatcher.TargetVDB.Spec.NMATLSSecret = "replication-start-test-target-secret"
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)
		test.CreateFakeTLSSecret(ctx, dispatcher.VDB, dispatcher.Client, dispatcher.TargetVDB.Spec.NMATLSSecret)
		defer test.DeleteSecret(ctx, dispatcher.Client, dispatcher.VDB.Spec.NMATLSSecret)

		_, err := dispatcher.ReplicateDB(ctx,
			replicationstart.WithSourceIP(TestSourceIP),
			replicationstart.WithTargetIP(TestTargetIP),
			replicationstart.WithSourceUsername(vapi.SuperUser),
			replicationstart.WithTargetDBName(TestTargetDBName),
			replicationstart.WithTargetUserName(TestTargetUserName),
			replicationstart.WithTargetPassword(TestTargetPassword),
			replicationstart.WithSourceTLSConfig(TestSourceTLSConfig),
			replicationstart.WithAsync(true),
			replicationstart.WithObjectName(TestTableOrSchemaName),
			replicationstart.WithIncludePattern(TestIncludePattern),
			replicationstart.WithExcludePattern(TestExcludePattern),
			replicationstart.WithTargetNamespace(TestTargetNamespace),
		)
		Ω(err).Should(Succeed())
	})
})
