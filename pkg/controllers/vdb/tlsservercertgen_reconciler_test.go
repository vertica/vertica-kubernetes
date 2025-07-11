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

package vdb

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("tlsservercertgen_reconcile", func() {
	ctx := context.Background()

	It("should be a no-op if not using vclusterops", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		vdb.Spec.HTTPSNMATLS.Secret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.GetTLSConfigByName(vapi.HTTPSNMATLSConfigName)).ShouldNot(Equal(""))
	})

	It("should be a no-op if not using vclusterops and secret name is set", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		const DummySecretName = "dummy"
		vdb.Spec.HTTPSNMATLS.Secret = DummySecretName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.GetHTTPSNMATLSSecret()).Should(Equal(DummySecretName))
	})

	It("should create a secret when http server is enabled and secret name is missing", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.HTTPSNMATLS.Secret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.GetHTTPSNMATLSSecret()).ShouldNot(Equal(""))
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.GetHTTPSNMATLSSecret()}
		secret := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
		Expect(len(secret.Data[corev1.TLSPrivateKeyKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[corev1.TLSCertKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[paths.HTTPServerCACrtName])).ShouldNot(Equal(0))
	})

	It("should recreate the secret if the name is set but it doesn't exist", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		const TLSSecretName = "recreate-secret-name"
		vdb.Spec.HTTPSNMATLS.Secret = TLSSecretName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.GetHTTPSNMATLSSecret()}
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, nm, secret)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.GetHTTPSNMATLSSecret()).Should(Equal(TLSSecretName))
		Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
		Expect(len(secret.Data[corev1.TLSPrivateKeyKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[corev1.TLSCertKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[paths.HTTPServerCACrtName])).ShouldNot(Equal(0))
	})
})
