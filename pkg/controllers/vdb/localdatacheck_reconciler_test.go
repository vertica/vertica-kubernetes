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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("localdatacheck_reconcile", func() {
	ctx := context.Background()

	It("should write events when disk space is low", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		prunner := &cmds.FakePodRunner{}
		// PodFacts should have 1 of 2 pods running low on space
		pfacts := createPodFactsDefault(vdb, prunner)
		Expect(pfacts.Collect(ctx)).Should(Succeed())
		sc := &vdb.Spec.Subclusters[0]
		pn := names.GenPodName(vdb, sc, 0)
		pfacts.Detail[pn].localDataAvail = 30 * 1024 * 1024
		pn = names.GenPodName(vdb, sc, 1)
		pfacts.Detail[pn].localDataAvail = 5 * 1024 * 1024
		pn = names.GenPodName(vdb, sc, 1)
		pfacts.Detail[pn].localDataAvail = 1 * 1024 * 1024 * 1024

		actor := MakeLocalDataCheckReconciler(vdbRec, vdb, pfacts)
		l := actor.(*LocalDataCheckReconciler)
		Expect(l.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(l.NumEvents).Should(Equal(1))
	})
})
