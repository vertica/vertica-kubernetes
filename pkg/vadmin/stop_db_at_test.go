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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
)

var _ = Describe("stop_db_at", func() {
	ctx := context.Background()

	It("should call admintools -t stop_db", func() {
		dispatcher, vdb, fpr := mockAdmintoolsDispatcher()
		nm := names.GenPodName(vdb, &vdb.Spec.Subclusters[0], 0)
		Ω(dispatcher.StopDB(ctx,
			stopdb.WithInitiator(nm, "10.9.1.1"),
		)).Should(Succeed())
		hist := fpr.FindCommands("-t stop_db")
		Ω(len(hist)).Should(Equal(1))
	})
})
