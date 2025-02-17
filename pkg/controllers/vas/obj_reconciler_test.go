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

//nolint:dupl
package vas

import (
	"context"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

const AuthSecret = "authsecret"

var _ = Describe("obj_reconcile", func() {
	ctx := context.Background()

	It("should create/update hpa", func() {
		vas := v1beta1.MakeVASWithMetrics()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteHPA(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		nm := names.GenHPAName(vas)
		Expect(k8sClient.Get(ctx, nm, hpa)).Should(Succeed())
		Expect(hpa.Spec.ScaleTargetRef.Name).Should(Equal(vas.Name))
		Expect(*hpa.Spec.MinReplicas).Should(Equal(*vas.Spec.CustomAutoscaler.Hpa.MinReplicas))
		Expect(hpa.Spec.MaxReplicas).Should(Equal(vas.Spec.CustomAutoscaler.Hpa.MaxReplicas))
		Expect(hpa.Spec.Metrics[0].Resource.Name).Should(Equal(corev1.ResourceName("cpu")))

		// Update hpa
		newRep := int32(10)
		vas.Spec.CustomAutoscaler.Hpa.MaxReplicas = newRep
		r = MakeObjReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, hpa)).Should(Succeed())
		Expect(hpa.Spec.MaxReplicas).Should(Equal(newRep))
	})

	It("should create/update scaledObject", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		so := &kedav1alpha1.ScaledObject{}
		nm := names.GenScaledObjectName(vas)
		Expect(k8sClient.Get(ctx, nm, so)).Should(Succeed())
		Expect(so.Spec.ScaleTargetRef.Name).Should(Equal(vas.Name))
		Expect(*so.Spec.MinReplicaCount).Should(Equal(*vas.Spec.CustomAutoscaler.ScaledObject.MinReplicas))
		Expect(*so.Spec.MaxReplicaCount).Should(Equal(*vas.Spec.CustomAutoscaler.ScaledObject.MaxReplicas))
		Expect(len(so.Spec.Triggers)).Should(Equal(len(vas.Spec.CustomAutoscaler.ScaledObject.Metrics)))
		metric := &vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0]
		Expect(so.Spec.Triggers[0].Metadata["serverAddress"]).Should(Equal(metric.Prometheus.ServerAddress))

		// Update scaledObject
		newRep := int32(10)
		vas.Spec.CustomAutoscaler.ScaledObject.MaxReplicas = &newRep
		r = MakeObjReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &ctrl.Request{})
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())
		Expect(k8sClient.Get(ctx, nm, so)).Should(Succeed())
		Expect(*so.Spec.MaxReplicaCount).Should(Equal(newRep))
	})

	It("should create triggerAuthentication object with auth basic method", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "basic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyUsername] = []byte("username")
		secret.Data[v1beta1.PrometheusSecretKeyPassword] = []byte("password")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"basic")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyUsername))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyPassword))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(2))
	})

	It("should should not create triggerAuthentication when metric type is not prometheus", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "notused"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"notused")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteHPA(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		// should not find triggerAuthentication created
		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(errors.IsNotFound(k8sClient.Get(ctx, taName, ta))).Should(BeTrue())
	})

	It("should fail to create triggerAuthentication object with auth basic method, when key is missing", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "basic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyUsername] = []byte("username")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"basic")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("password not found in secret"))
	})

	It("should create triggerAuthentication object with auth bearer method", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthBearer
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "bearer"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyBearerToken] = []byte("bearertoken")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"bearer")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyBearerToken))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(1))
	})

	It("should fail to create triggerAuthentication object with auth bearertoken method, when key is missing", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthBearer
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "bearer"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"bearer")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("bearerToken not found in secret"))
	})

	It("should create triggerAuthentication object with auth TLS method", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthTLS
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "tls"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyCa] = []byte("ca")
		secret.Data[v1beta1.PrometheusSecretKeyCert] = []byte("cert")
		secret.Data[v1beta1.PrometheusSecretKeyKey] = []byte("key")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"tls")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCa))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCert))
		Expect(ta.Spec.SecretTargetRef[2].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyKey))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(3))
	})

	It("should fail to create triggerAuthentication object with auth tls method, when key is missing", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthTLS
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "tls"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"tls")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("ca not found in secret"))
	})

	It("should create triggerAuthentication object with auth custom method", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthCustom
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "custom"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyCustomAuthHeader] = []byte("auththeader")
		secret.Data[v1beta1.PrometheusSecretKeyCustomAuthValue] = []byte("authvalue")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"custom")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCustomAuthHeader))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCustomAuthValue))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(2))
	})

	It("should fail to create triggerAuthentication object with auth custom method, when key is missing", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthCustom
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "custom"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"custom")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("customAuthHeader not found in secret"))
	})

	It("should create triggerAuthentication object with auth tls,basic method", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthTLSAndBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "tlsbasic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[v1beta1.PrometheusSecretKeyUsername] = []byte("username")
		secret.Data[v1beta1.PrometheusSecretKeyPassword] = []byte("password")
		secret.Data[v1beta1.PrometheusSecretKeyCa] = []byte("ca")
		secret.Data[v1beta1.PrometheusSecretKeyCert] = []byte("cert")
		secret.Data[v1beta1.PrometheusSecretKeyKey] = []byte("key")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"tlsbasic")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyUsername))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyPassword))
		Expect(ta.Spec.SecretTargetRef[2].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCa))
		Expect(ta.Spec.SecretTargetRef[3].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyCert))
		Expect(ta.Spec.SecretTargetRef[4].Key).Should(ContainSubstring(v1beta1.PrometheusSecretKeyKey))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(5))
	})

	It("should fail to create triggerAuthentication object with auth tls,basic method, when key is missing", func() {
		vas := v1beta1.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = v1beta1.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = v1beta1.PrometheusAuthTLSAndBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = AuthSecret + "tlsbasic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, AuthSecret+"tlsbasic")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("username not found in secret"))
	})
})
