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

package sandbox

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("verifydeployment_reconciler", func() {
	ctx := context.Background()

	It("should reconcile based on the deployment and version", func() {
		vdb := v1.MakeVDBForVclusterOps()
		vdb.Annotations[vmeta.VersionAnnotation] = v1.SandboxSupportedMinVersion

		r := MakeVerifyDeploymentReconciler(sbRec, vdb, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))

		vdb.Annotations[vmeta.VersionAnnotation] = "v24.2.0"
		r = MakeVerifyDeploymentReconciler(sbRec, vdb, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))

		vdb.Annotations[vmeta.VersionAnnotation] = v1.SandboxSupportedMinVersion
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		r = MakeVerifyDeploymentReconciler(sbRec, vdb, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	})
})
