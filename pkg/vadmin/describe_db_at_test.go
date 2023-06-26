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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("describe_db_at", func() {
	ctx := context.Background()

	It("should call admintools -t revive_db with --display-only", func() {
		dispatcher, _, fpr := mockAdmintoolsDispatcher()
		_, res, err := dispatcher.DescribeDB(ctx,
			describedb.WithCommunalPath("/communal"),
			describedb.WithDBName("blah"),
		)
		Ω(err).Should(Succeed())
		Ω(res).Should(Equal(ctrl.Result{}))
		hist := fpr.FindCommands("-t revive_db")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement("--display-only"))
		Ω(hist[0].Command).Should(ContainElement("blah"))
	})
})
