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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("tlsservercertgen_reconcile", func() {
	ctx := context.Background()
	const trueStr = "true"
	const falseStr = "false"

	It("should be a op if not using vclusterops", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		vdb.Spec.HTTPSTLSSecret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPSTLSSecret).ShouldNot(Equal(""))
	})

	It("should be a no-op if not using vclusterops and secret name is set", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationFalse
		const DummySecretName = "dummy"
		vdb.Spec.HTTPSTLSSecret = DummySecretName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPSTLSSecret).Should(Equal(DummySecretName))
	})

	It("should create a secret when http server is enabled and secret name is missing", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.VClusterOpsAnnotation] = vmeta.VClusterOpsAnnotationTrue
		vdb.Spec.HTTPSTLSSecret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPSTLSSecret).ShouldNot(Equal(""))
		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Spec.HTTPSTLSSecret}
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
		vdb.Spec.HTTPSTLSSecret = TLSSecretName
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		nm := types.NamespacedName{Namespace: vdb.Namespace, Name: vdb.Spec.HTTPSTLSSecret}
		secret := &corev1.Secret{}
		err := k8sClient.Get(ctx, nm, secret)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.HTTPSTLSSecret).Should(Equal(TLSSecretName))
		Expect(k8sClient.Get(ctx, nm, secret)).Should(Succeed())
		Expect(len(secret.Data[corev1.TLSPrivateKeyKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[corev1.TLSCertKey])).ShouldNot(Equal(0))
		Expect(len(secret.Data[paths.HTTPServerCACrtName])).ShouldNot(Equal(0))

	})

	It("should be a no-op if UseNMACertsMount is enabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = trueStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = falseStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		objr := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		r := objr.(*TLSServerCertGenReconciler)
		err := r.reconcileNMACertConfigMap(ctx)
		Expect(err).Should(Succeed())
	})

	It("should be a no-op if TLSCertsRotation is disabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = falseStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		objr := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		r := objr.(*TLSServerCertGenReconciler)
		err := r.reconcileNMACertConfigMap(ctx)
		Expect(err).Should(Succeed())
	})

	It("should create the ConfigMap if it does not exist", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = trueStr
		const existing = "existing-secret"
		vdb.Spec.NMATLSSecret = existing
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		// Ensure the ConfigMap doesn't exist
		configMapName := names.GenNMACertConfigMap(vdb)
		configMap := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, configMapName, configMap)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		objr := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		r := objr.(*TLSServerCertGenReconciler)
		err = r.reconcileNMACertConfigMap(ctx)
		defer deleteConfigMap(ctx, vdb, configMapName.Name)
		Expect(err).Should(Succeed())

		// Verify that the ConfigMap was created
		err = k8sClient.Get(ctx, configMapName, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal(vdb.Spec.NMATLSSecret))
	})

	It("should update the ConfigMap if the secret name changes", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = trueStr
		const initial = "initial-secret"
		vdb.Spec.NMATLSSecret = initial
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		nm := names.GenNMACertConfigMap(vdb)
		configMap := builder.BuildNMATLSConfigMap(nm, vdb)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())
		defer deleteConfigMap(ctx, vdb, nm.Name)

		vdb.Spec.NMATLSSecret = "updated-secret"
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		objr := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		r := objr.(*TLSServerCertGenReconciler)
		err := r.reconcileNMACertConfigMap(ctx)
		Expect(err).Should(Succeed())

		// Verify that the ConfigMap was updated
		err = k8sClient.Get(ctx, nm, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal("updated-secret"))
	})

})
