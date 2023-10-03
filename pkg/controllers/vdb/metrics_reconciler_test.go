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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("prometheus_reconcile", func() {
	ctx := context.Background()

	It("should collect prometheus gauge values based on the pod facts", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.Subclusters[0].Size = 3
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		prunner := &cmds.FakePodRunner{}
		pfacts := createPodFactsDefault(prunner)
		actor := MakeMetricReconciler(vdbRec, logger, vdb, prunner, pfacts)
		r := actor.(*MetricReconciler)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		metrics := r.captureRawMetrics()
		for oid := range metrics {
			Expect(metrics[oid].podCount).Should(Equal(float64(3)))
			Expect(metrics[oid].runningCount).Should(Equal(float64(3)))
			Expect(metrics[oid].readyCount).Should(Equal(float64(3)))
		}
	})
})
