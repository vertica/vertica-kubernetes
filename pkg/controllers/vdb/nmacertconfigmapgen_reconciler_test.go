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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("nmacertconfigmapgen_reconcile", func() {
	ctx := context.Background()
	const trueStr = "true"
	const falseStr = "false"

	It("should be a no-op if UseNMACertsMount is enabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = trueStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = falseStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeNMACertConfigMapGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should be a no-op if TLSCertsRotation is disabled", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = falseStr
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeNMACertConfigMapGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
	})

	It("should requeue if secret name is not set", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = trueStr
		vdb.Spec.NMATLSSecret = ""
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeNMACertConfigMapGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{Requeue: true}))
	})

	It("should create the ConfigMap if it does not exist", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSCertsRotationAnnotation] = trueStr
		const existing = "existing-secret"
		vdb.Spec.NMATLSSecret = existing
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.NMATLSSecret).Should(Equal(existing))

		// Ensure the ConfigMap doesn't exist
		configMapName := fmt.Sprintf("%s-%s", vdb.Name, vapi.NMATLSConfigMapName)
		configMap := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: vdb.Namespace}, configMap)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		r = MakeNMACertConfigMapGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		defer deleteConfigMap(ctx, vdb, configMapName)

		// Verify that the ConfigMap was created
		err = k8sClient.Get(ctx, types.NamespacedName{Name: configMapName, Namespace: vdb.Namespace}, configMap)
		Expect(err).ShouldNot(HaveOccurred())
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

		r := MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.NMATLSSecret).Should(Equal(initial))

		nm := fmt.Sprintf("%s-%s", vdb.Name, vapi.NMATLSConfigMapName)
		configMap := builder.BuildNMATLSConfigMap(nm, vdb)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())
		defer deleteConfigMap(ctx, vdb, nm)

		vdb.Spec.NMATLSSecret = "updated-secret"
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		r = MakeTLSServerCertGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))
		Expect(vdb.Spec.NMATLSSecret).Should(Equal("updated-secret"))

		r = MakeNMACertConfigMapGenReconciler(vdbRec, logger, vdb)
		Expect(r.Reconcile(ctx, &ctrl.Request{})).Should(Equal(ctrl.Result{}))

		// Verify that the ConfigMap was updated
		err := k8sClient.Get(ctx, types.NamespacedName{Name: nm, Namespace: vdb.Namespace}, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal("updated-secret"))
	})
})
