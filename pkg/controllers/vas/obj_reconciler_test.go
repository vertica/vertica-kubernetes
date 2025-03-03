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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

const authSecret = "authsecret"

var _ = Describe("obj_reconcile", func() {
	ctx := context.Background()

	It("should create/update hpa", func() {
		vas := vapi.MakeVASWithMetrics()
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
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
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
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "basic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyUsername] = []byte("username")
		secret.Data[vapi.PrometheusSecretKeyPassword] = []byte("password")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"basic")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyUsername))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyPassword))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(2))
	})

	It("should should not create triggerAuthentication when metric type is not prometheus", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "notused"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"notused")

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
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "basic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyUsername] = []byte("username")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"basic")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("password not found in secret"))
	})

	It("should create triggerAuthentication object with auth bearer method", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthBearer
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "bearer"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyBearerToken] = []byte("bearertoken")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"bearer")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyBearerToken))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(1))
	})

	It("should fail to create triggerAuthentication object with auth bearertoken method, when key is missing", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthBearer
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "bearer"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"bearer")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("bearerToken not found in secret"))
	})

	It("should create triggerAuthentication object with auth TLS method", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthTLS
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "tls"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyCa] = []byte("ca")
		secret.Data[vapi.PrometheusSecretKeyCert] = []byte("cert")
		secret.Data[vapi.PrometheusSecretKeyKey] = []byte("key")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"tls")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCa))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCert))
		Expect(ta.Spec.SecretTargetRef[2].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyKey))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(3))
	})

	It("should fail to create triggerAuthentication object with auth tls method, when key is missing", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthTLS
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "tls"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"tls")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("ca not found in secret"))
	})

	It("should create triggerAuthentication object with auth custom method", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthCustom
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "custom"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyCustomAuthHeader] = []byte("auththeader")
		secret.Data[vapi.PrometheusSecretKeyCustomAuthValue] = []byte("authvalue")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"custom")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCustomAuthHeader))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCustomAuthValue))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(2))
	})

	It("should fail to create triggerAuthentication object with auth custom method, when key is missing", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthCustom
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "custom"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"custom")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("customAuthHeader not found in secret"))
	})

	It("should create triggerAuthentication object with auth tls,basic method", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthTLSAndBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "tlsbasic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		secret.Data[vapi.PrometheusSecretKeyUsername] = []byte("username")
		secret.Data[vapi.PrometheusSecretKeyPassword] = []byte("password")
		secret.Data[vapi.PrometheusSecretKeyCa] = []byte("ca")
		secret.Data[vapi.PrometheusSecretKeyCert] = []byte("cert")
		secret.Data[vapi.PrometheusSecretKeyKey] = []byte("key")
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"tlsbasic")

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		ta := &kedav1alpha1.TriggerAuthentication{}
		taName := names.GenTriggerAuthenticationtName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		Expect(k8sClient.Get(ctx, taName, ta)).Should(Succeed())
		Expect(ta.Spec.SecretTargetRef[0].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyUsername))
		Expect(ta.Spec.SecretTargetRef[1].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyPassword))
		Expect(ta.Spec.SecretTargetRef[2].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCa))
		Expect(ta.Spec.SecretTargetRef[3].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyCert))
		Expect(ta.Spec.SecretTargetRef[4].Key).Should(ContainSubstring(vapi.PrometheusSecretKeyKey))
		Expect(len(ta.Spec.SecretTargetRef)).Should(Equal(5))
	})

	It("should fail to create triggerAuthentication object with auth tls,basic method, when key is missing", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = vapi.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.AuthModes = vapi.PrometheusAuthTLSAndBasic
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret = authSecret + "tlsbasic"
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		secretName := names.GenAuthSecretName(vas, vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].AuthSecret)
		secret := builder.BuildSecretBase(secretName)
		Expect(k8sClient.Create(ctx, secret)).Should(Succeed())
		defer deleteSecret(ctx, vas, authSecret+"tlsbasic")

		r := MakeObjReconciler(vasRec, vas, logger)
		_, err := r.Reconcile(ctx, &ctrl.Request{})
		Expect(err).ShouldNot(Succeed())
		Expect(err).To(MatchError("username not found in secret"))
	})
})
