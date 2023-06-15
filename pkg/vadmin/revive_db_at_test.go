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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/mgmterrors"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("revive_db", func() {
	ctx := context.Background()

	It("should call admintools -t revive_db", func() {
		vdb := vapi.MakeVDB()
		fpr := &cmds.FakePodRunner{}
		evWriter := mgmterrors.TestEVWriter{}
		dispatcher := MakeAdmintools(logger, vdb, fpr, &evWriter)
		Ω(dispatcher.ReviveDB(ctx,
			revivedb.WithCommunalPath("/communal-1"),
			revivedb.WithDBName("testdb"),
		)).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("-t revive_db")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement("--communal-storage-location=/communal-1"))
		Ω(hist[0].Command).Should(ContainElement("testdb"))
	})
})
