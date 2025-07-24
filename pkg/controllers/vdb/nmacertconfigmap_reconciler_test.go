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
	"github.com/vertica/vertica-kubernetes/pkg/test"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

var _ = Describe("nmacertconfigmap_reconcile", func() {
	ctx := context.Background()
	const trueStr = "true"
	const falseStr = "false"

	It("should create configmap even when tls is not enabled", func() {
		const existing = "existing-secret"
		vdb := vapi.MakeVDBForHTTP(existing)
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = trueStr
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = falseStr

		configMapName := names.GenNMACertConfigMap(vdb)
		configMap := &corev1.ConfigMap{}

		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)
		defer deleteConfigMap(ctx, vdb, configMapName.Name)

		// Ensure the ConfigMap doesn't exist
		err := k8sClient.Get(ctx, configMapName, configMap)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		objr := MakeNMACertConfigMapReconciler(vdbRec, logger, vdb)
		r := objr.(*NMACertConfigMapReconciler)
		_, err = r.Reconcile(ctx, nil)
		Expect(err).Should(Succeed())
		// Verify that the ConfigMap was created
		err = k8sClient.Get(ctx, configMapName, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal(vdb.GetNMATLSSecret()))
	})

	It("should create the ConfigMap if it does not exist", func() {
		vdb := vapi.MakeVDB()
		vdb.Annotations[vmeta.MountNMACertsAnnotation] = falseStr
		vdb.Annotations[vmeta.EnableTLSAuthAnnotation] = trueStr
		const existing = "existing-secret"
		vdb.Spec.HTTPSNMATLS.Secret = existing
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		// Ensure the ConfigMap doesn't exist
		configMapName := names.GenNMACertConfigMap(vdb)
		configMap := &corev1.ConfigMap{}
		err := k8sClient.Get(ctx, configMapName, configMap)
		Expect(errors.IsNotFound(err)).Should(BeTrue())

		objr := MakeNMACertConfigMapReconciler(vdbRec, logger, vdb)
		r := objr.(*NMACertConfigMapReconciler)
		_, err = r.Reconcile(ctx, nil)
		defer deleteConfigMap(ctx, vdb, configMapName.Name)
		Expect(err).Should(Succeed())

		// Verify that the ConfigMap was created
		err = k8sClient.Get(ctx, configMapName, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal(vdb.GetHTTPSNMATLSSecret()))
	})

	It("should update the ConfigMap if the secret name changes", func() {
		vdb := vapi.MakeVDB()
		vapi.SetVDBForTLS(vdb)
		const initial = "initial-secret"
		vdb.Spec.HTTPSNMATLS.Secret = initial
		test.CreateVDB(ctx, k8sClient, vdb)
		defer test.DeleteVDB(ctx, k8sClient, vdb)

		nm := names.GenNMACertConfigMap(vdb)
		configMap := builder.BuildNMATLSConfigMap(nm, vdb)
		Expect(k8sClient.Create(ctx, configMap)).Should(Succeed())
		defer deleteConfigMap(ctx, vdb, nm.Name)

		vdb.Spec.HTTPSNMATLS.Secret = "updated-secret"
		Expect(k8sClient.Update(ctx, vdb)).Should(Succeed())

		objr := MakeNMACertConfigMapReconciler(vdbRec, logger, vdb)
		r := objr.(*NMACertConfigMapReconciler)
		_, err := r.Reconcile(ctx, nil)
		Expect(err).Should(Succeed())

		// Verify that the ConfigMap was updated
		err = k8sClient.Get(ctx, nm, configMap)
		Expect(err).Should(Succeed())
		Expect(configMap.Data[builder.NMASecretNameEnv]).Should(Equal("updated-secret"))
	})

})
