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

var _ = Describe("httpserverctrl_reconcile", func() {
	ctx := context.Background()

	It("should start the http server if it isn't running", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vapi.VersionAnnotation] = vapi.HTTPServerMinVersion
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeEnabled
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindHTTPServerStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(3))
	})

	It("should start the http server, when auto, only if on supported version", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vapi.VersionAnnotation] = vapi.HTTPServerMinVersion
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeAuto
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindHTTPServerStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(0))

		vdb.Annotations[vapi.VersionAnnotation] = vapi.HTTPServerAutoMinVersion
		cmds = reconcileAndFindHTTPServerStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(3))
	})

	It("should not try to start the http server if server version does not support it", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vapi.VersionAnnotation] = "v12.0.2"
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeEnabled
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindHTTPServerStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(0))
	})

	It("should not try to start the http server if disabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vapi.VersionAnnotation] = vapi.HTTPServerMinVersion
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeDisabled
		test.CreatePods(ctx, k8sClient, vdb, test.AllPodsRunning)
		defer test.DeletePods(ctx, k8sClient, vdb)

		cmds := reconcileAndFindHTTPServerStart(ctx, vdb)
		Expect(len(cmds)).Should(Equal(0))
	})
})

func reconcileAndFindHTTPServerStart(ctx context.Context, vdb *vapi.VerticaDB) []cmds.CmdHistory {
	fpr := &cmds.FakePodRunner{}
	pfacts := createPodFactsWithHTTPServerNotRunning(ctx, vdb, fpr)
	h := MakeHTTPServerCtrlReconciler(vdbRec, logger, vdb, fpr, pfacts)
	Expect(h.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	return fpr.FindCommands("vsql", "-tAc", genHTTPServerCtrlQuery("start"))
}
