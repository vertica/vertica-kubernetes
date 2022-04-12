/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
)

var _ = Describe("at", func() {
	ctx := context.Background()

	It("should copy to all pods if copying admintools.conf fails at one of the pods", func() {
		vdb := vapi.MakeVDB()
		sc := &vdb.Spec.Subclusters[0]
		const ScSize = 3
		sc.Size = ScSize
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		fpr := &cmds.FakePodRunner{}
		pf := createPodFactsWithInstallNeeded(ctx, vdb, fpr)
		fpr.Results = cmds.CmdResults{
			names.GenPodName(vdb, sc, 2): []cmds.CmdResult{
				{Err: fmt.Errorf("failed to copy file")},
			},
		}
		Expect(distributeAdmintoolsConf(ctx, vdb, vdbRec, pf, fpr, "at.conf.tmp")).ShouldNot(Succeed())
		cmds := fpr.FindCommands(fmt.Sprintf("sh -c cat > %s", paths.AdminToolsConf))
		Expect(len(cmds)).Should(Equal(ScSize))
	})
})
