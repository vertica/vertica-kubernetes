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

package vscr

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	test "github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("scrutinizepod_reconciler", func() {
	ctx := context.Background()

	It("should reconcile successfully", func() {
		vdb := v1beta1.MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = v1.VcluseropsAsDefaultDeploymentMethodMinVersion
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeVDBVerifyReconciler(vscrRec, vscr, logger, &version.Info{})
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{}))
	})

	It("should return no error if server version does not support vcluster", func() {
		vdb := v1beta1.MakeVDB()
		vdb.Annotations[vmeta.VersionAnnotation] = "v23.4.0"
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeVDBVerifyReconciler(vscrRec, vscr, logger, &version.Info{})
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
	})

	It("should requeue if vdb does not have server version info yet", func() {
		vdb := v1beta1.MakeVDB()
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeVDBVerifyReconciler(vscrRec, vscr, logger, &version.Info{})
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should requeue if vdb does not exist", func() {
		vscr := v1beta1.MakeVscr()
		test.CreateVSCR(ctx, k8sClient, vscr)
		defer test.DeleteVSCR(ctx, k8sClient, vscr)

		r := MakeVDBVerifyReconciler(vscrRec, vscr, logger, &version.Info{})
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).Should(Succeed())
		Expect(res).Should(Equal(ctrl.Result{Requeue: true}))
	})
})
