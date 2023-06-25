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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
)

var _ = Describe("remove_sc_at", func() {
	ctx := context.Background()

	It("should call admintools -t db_add_subcluster", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(dispatcher.AddSubcluster(ctx,
			addsc.WithInitiator(nm, "10.9.1.93"),
			addsc.WithSubcluster(vdb.Spec.Subclusters[0].Name),
		)).Should(Succeed())
		hist := fpr.FindCommands("-t db_add_subcluster")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement(vdb.Spec.Subclusters[0].Name))
	})
})
