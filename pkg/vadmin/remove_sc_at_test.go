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
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
)

var _ = Describe("remove_sc_at", func() {
	ctx := context.Background()

	It("should call admintools -t db_remove_subcluster", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(dispatcher.RemoveSubcluster(ctx,
			removesc.WithInitiator(nm, "10.9.1.92"),
			removesc.WithSubcluster(vdb.Spec.Subclusters[0].Name),
		)).Should(Succeed())
		hist := fpr.FindCommands("-t db_remove_subcluster")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement(vdb.Spec.Subclusters[0].Name))
	})

	It("should be a no-op if the subcluster is already gone", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		fpr.Results[nm] = []cmds.CmdResult{
			{
				Err:    errors.New("admintools command failed"),
				Stdout: "No subcluster found for the given name",
			},
		}
		Ω(dispatcher.RemoveSubcluster(ctx,
			removesc.WithInitiator(nm, "10.9.1.92"),
			removesc.WithSubcluster("notexist"),
		)).Should(Succeed())
		hist := fpr.FindCommands("-t db_remove_subcluster")
		Ω(len(hist)).Should(Equal(1))
		Ω(hist[0].Command).Should(ContainElement("notexist"))
	})
})
