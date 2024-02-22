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
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("revive_db_at", func() {
	ctx := context.Background()

	It("should call admintools -t revive_db", func() {
		dispatcher, _, fpr := mockAdmintoolsDispatcher()
		Ω(dispatcher.ReviveDB(ctx,
			revivedb.WithCommunalPath("/communal-1"),
			revivedb.WithDBName("testdb"),
		)).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("-t revive_db")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement("--communal-storage-location=/communal-1"))
		Ω(hist[0].Command).Should(ContainElement("testdb"))
	})

	It("should create a non empty auth file", func() {
		confParms := map[string]string{
			TestParm: TestValue,
		}
		dispatcher, _, fpr := mockAdmintoolsDispatcher()
		res, err := dispatcher.ReviveDB(ctx,
			revivedb.WithCommunalPath("/communal"),
			revivedb.WithConfigurationParams(confParms),
		)
		createNonEmptyFileHelper(res, err, fpr)
	})

	It("should delete auth file at the end", func() {
		confParms := map[string]string{
			TestParm: TestValue,
		}
		dispatcher, _, fpr := mockAdmintoolsDispatcher()
		res, err := dispatcher.ReviveDB(ctx,
			revivedb.WithCommunalPath("/communal"),
			revivedb.WithConfigurationParams(confParms),
		)
		Ω(err).Should(Succeed())
		Ω(res).Should(Equal(ctrl.Result{}))
		cmd := fmt.Sprintf("rm %s", paths.AuthParmsFile)
		hist := fpr.FindCommands(cmd)
		Ω(len(hist)).Should(Equal(1))
	})
})
