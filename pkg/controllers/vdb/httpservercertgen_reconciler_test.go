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
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("httpservercertgen_reconcile", func() {
	ctx := context.Background()

	It("should be a no-op if http server isn't enabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeDisabled
		vdb.Spec.HTTPServerTLSSecret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeHTTPServerCertGenReconciler(vdbRec, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPServerTLSSecret).Should(Equal(""))
	})

	It("should be a no-op if http server is disabled and secret name is set", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeDisabled
		const DummySecretName = "dummy"
		vdb.Spec.HTTPServerTLSSecret = DummySecretName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeHTTPServerCertGenReconciler(vdbRec, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPServerTLSSecret).Should(Equal(DummySecretName))
	})

	It("should create a secret when http server is enabled and secret name is missing", func() {
		vdb := vapi.MakeVDB()
		vdb.Spec.HTTPServerMode = vapi.HTTPServerModeEnabled
		vdb.Spec.HTTPServerTLSSecret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeHTTPServerCertGenReconciler(vdbRec, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPServerTLSSecret).ShouldNot(Equal(""))
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Spec.HTTPServerTLSSecret}
		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
		Expect(len(secret.Data[corev1.TLSPrivateKeyKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[corev1.TLSCertKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[paths.HTTPServerCACrtName])).ShouldNot(Equal(0))
	})
})
