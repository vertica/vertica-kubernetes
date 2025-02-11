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

package vas

import (
	"context"

	kedav1alpha1 "github.com/kedacore/keda/v2/apis/keda/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/v1beta1_test"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

var _ = Describe("scaledown_reconcile", func() {
	ctx := context.Background()

	It("should requeue if hpa is not ready", func() {
		vas := vapi.MakeVASWithMetrics()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteHPA(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		req := ctrl.Request{NamespacedName: vapi.MakeVASName()}
		r = MakeVerifyAutoscalerReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &req)
		Expect(res.Requeue).Should(BeTrue())
		Expect(err).Should(Succeed())

		hpa := &autoscalingv2.HorizontalPodAutoscaler{}
		nm := names.GenHPAName(vas)
		Expect(k8sClient.Get(ctx, nm, hpa)).Should(Succeed())
		hpa.Status.Conditions = []autoscalingv2.HorizontalPodAutoscalerCondition{
			{
				Type:               autoscalingv2.ScalingActive,
				Status:             corev1.ConditionTrue,
				LastTransitionTime: metav1.Now(),
			},
		}
		Expect(k8sClient.Status().Update(ctx, hpa)).Should(Succeed())
		r = MakeVerifyAutoscalerReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &req)
		Expect(res.Requeue).Should(BeTrue())
		Expect(err).Should(Succeed())

		curCPU := int32(55)
		hpa.Status.CurrentMetrics = []autoscalingv2.MetricStatus{
			{
				Type: autoscalingv2.ResourceMetricSourceType,
				Resource: &autoscalingv2.ResourceMetricStatus{
					Name: corev1.ResourceCPU,
					Current: autoscalingv2.MetricValueStatus{
						AverageUtilization: &curCPU,
					},
				},
			},
		}
		Expect(k8sClient.Status().Update(ctx, hpa)).Should(Succeed())
		r = MakeVerifyAutoscalerReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &req)
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		fetchVas := &v1beta1.VerticaAutoscaler{}
		Expect(k8sClient.Get(ctx, v1beta1.MakeVASName(), fetchVas)).Should(Succeed())
		Expect(len(fetchVas.Status.Conditions)).Should(Equal(2))
		Expect(fetchVas.Status.Conditions[1].Type).Should(Equal(v1beta1.ScalingActive))
		Expect(fetchVas.Status.Conditions[1].Status).Should(Equal(corev1.ConditionTrue))
	})

	It("should requeue if scaledObject is not ready", func() {
		vas := vapi.MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa = nil
		vas.Spec.CustomAutoscaler.Type = v1beta1.ScaledObject
		vas.Spec.CustomAutoscaler.ScaledObject = vapi.MakeScaledObjectSpec()
		v1beta1_test.CreateVAS(ctx, k8sClient, vas)
		defer v1beta1_test.DeleteVAS(ctx, k8sClient, vas)

		r := MakeObjReconciler(vasRec, vas, logger)
		res, err := r.Reconcile(ctx, &ctrl.Request{})
		defer v1beta1_test.DeleteScaledObject(ctx, k8sClient, vas)
		Expect(res).Should(Equal(ctrl.Result{}))
		Expect(err).Should(Succeed())

		req := ctrl.Request{NamespacedName: v1beta1.MakeVASName()}
		r = MakeVerifyAutoscalerReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &req)
		Expect(res.Requeue).Should(BeTrue())
		Expect(err).Should(Succeed())

		so := &kedav1alpha1.ScaledObject{}
		nm := names.GenScaledObjectName(vas)
		Expect(k8sClient.Get(ctx, nm, so)).Should(Succeed())
		so.Status.Conditions = kedav1alpha1.Conditions{
			{
				Type:   kedav1alpha1.ConditionReady,
				Status: metav1.ConditionTrue,
			},
		}
		Expect(k8sClient.Status().Update(ctx, so)).Should(Succeed())
		r = MakeVerifyAutoscalerReconciler(vasRec, vas, logger)
		res, err = r.Reconcile(ctx, &req)
		Expect(err).Should(Succeed())
		Expect(res.Requeue).Should(BeFalse())
		fetchVas := &vapi.VerticaAutoscaler{}
		Expect(k8sClient.Get(ctx, vapi.MakeVASName(), fetchVas)).Should(Succeed())
		Expect(len(fetchVas.Status.Conditions)).Should(Equal(2))
		Expect(fetchVas.Status.Conditions[1].Type).Should(Equal(vapi.ScalingActive))
		Expect(fetchVas.Status.Conditions[1].Status).Should(Equal(corev1.ConditionTrue))
	})
})
