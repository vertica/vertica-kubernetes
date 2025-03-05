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

package v1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

var _ = Describe("verticaautoscaler_webhook", func() {
	It("should succeed with all valid fields", func() {
		vas := MakeVAS()
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
	})

	It("should fail if granularity isn't set properly", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = "BadValue"
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should not allow setting of CustomAutoscaler after Autoscaler creation", func() {
		oldVas := MakeVAS()
		oldVas.Spec.CustomAutoscaler = nil
		newVas := MakeVAS()
		newVas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: HPA,
			Hpa:  &HPASpec{},
		}

		Ω(newVas.validateImmutableFields(oldVas)).Should(HaveLen(1))
	})

	It("should fail if the service name differs", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = SubclusterScalingGranularity
		vas.Spec.Template.ServiceName = "SomethingElse"
		vas.Spec.Template.Size = 1
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		vas.Spec.Template.ServiceName = ""
		_, err1 := vas.ValidateCreate()
		Expect(err1).ShouldNot(Succeed())
		_, err2 := vas.ValidateUpdate(MakeVAS())
		Expect(err2).ShouldNot(Succeed())
		vas.Spec.Template.ServiceName = vas.Spec.ServiceName
		_, err3 := vas.ValidateUpdate(MakeVAS())
		Expect(err3).Should(Succeed())
		vas.Spec.ServiceName = ""
		vas.Spec.ScalingGranularity = PodScalingGranularity
		_, err4 := vas.ValidateUpdate(MakeVAS())
		Expect(err4).ShouldNot(Succeed())
	})

	It("should fail if you try to use the template with pod scalingGranularity", func() {
		vas := MakeVAS()
		vas.Spec.Template.ServiceName = vas.Spec.ServiceName
		vas.Spec.Template.Size = 1
		vas.Spec.ScalingGranularity = PodScalingGranularity
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		vas.Spec.ScalingGranularity = SubclusterScalingGranularity
		_, err1 := vas.ValidateCreate()
		Expect(err1).Should(Succeed())
	})

	It("maxReplicas must be set and cannot be less than minReplicas", func() {
		vas := MakeVAS()
		var minReplicas int32 = 5
		var maxReplicas int32 = 0

		// HPA
		vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: HPA,
			Hpa: &HPASpec{
				MinReplicas: &minReplicas,
				MaxReplicas: maxReplicas,
			},
		}
		_, err := vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("maxReplicas must be set"))

		maxReplicas = 3
		vas.Spec.CustomAutoscaler.Hpa.MaxReplicas = maxReplicas
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("cannot be less than minReplicas"))

		// ScaleObject
		vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: ScaledObject,
			ScaledObject: &ScaledObjectSpec{
				MinReplicas: &minReplicas,
			},
		}
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("maxReplicas must be set"))

		vas.Spec.CustomAutoscaler.ScaledObject.MaxReplicas = &maxReplicas
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("cannot be less than minReplicas"))

	})

	It("should fail if scaledobject metrics type is not set properly", func() {
		vas := MakeVASWithScaledObject()
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = "BadValue"
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if two metrics have the same name", func() {
		vas := MakeVASWithScaledObject()
		var maxReplicas int32 = 3
		var minReplicas int32 = 5
		vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: ScaledObject,
			ScaledObject: &ScaledObjectSpec{
				MinReplicas: &minReplicas,
				MaxReplicas: &maxReplicas,
				Metrics: []ScaleTrigger{
					{Name: "vertica_queued_requests_count"},
					{Name: "vertica_queued_requests_count"},
				},
			},
		}
		_, err := vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("cannot be the same"))
	})

	It("should fail if scaledobject metrics type is prometheus and metrics[].prometheus is nil", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = PrometheusTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus = nil
		_, err := vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("metrics[].prometheus must be set"))
	})

	It("should fail if scaledobject metrics type is cpu/mem and metrics[].resource is nil", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = CPUTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Resource = nil
		_, err := vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("metrics[].resource must be set"))

		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = MemTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Resource = nil
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("metrics[].resource must be set"))
	})

	It("should fail if scaledobject metrics type is prometheus and metricType is not set properly", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = PrometheusTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.ValueMetricType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus = &PrometheusSpec{}
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.AverageValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.UtilizationMetricType
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if scaledobject metrics type is cpu/mem and metricType is not set properly", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = CPUTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.UtilizationMetricType
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.AverageValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.ValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = MemTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.UtilizationMetricType
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.AverageValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.ValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if scaleInThreshold type is different to the threshold type used for scale out", func() {
		vas := MakeVASWithMetrics()
		vas.Spec.CustomAutoscaler.Hpa.Metrics[0].ScaleInThreshold = &autoscalingv2.MetricTarget{Type: autoscalingv2.UtilizationMetricType}
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.Hpa.Metrics[0].ScaleInThreshold.Type = autoscalingv2.ValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("must be of the same type as the threshold used for scale out"))
		vas.Spec.CustomAutoscaler.Hpa.Metrics[0].ScaleInThreshold.Type = autoscalingv2.AverageValueMetricType
		_, err = vas.ValidateCreate()
		Expect(err.Error()).To(ContainSubstring("must be of the same type as the threshold used for scale out"))
	})

	It("should fail if customAutoscaler.Hpa is nil and customAutoscaler.type is HPA", func() {
		vas := MakeVAS()
		vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: HPA,
		}
		vas.Spec.CustomAutoscaler.Hpa = nil
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if customAutoscaler.ScaledObject is nil and customAutoscaler.type is ScaledObject", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: ScaledObject,
		}
		vas.Spec.CustomAutoscaler.ScaledObject = nil
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())

	})

	It("should fail if scaledownThreshold is set, scaledown stabilization window is not 0", func() {
		vas := MakeVASWithMetrics()
		validValue := int32(0)
		invalidValue := int32(3)
		vas.Spec.CustomAutoscaler.Hpa.Metrics[0].ScaleInThreshold = &autoscalingv2.MetricTarget{
			Type: autoscalingv2.UtilizationMetricType,
		}
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.Hpa.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &validValue,
			},
		}
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.Hpa.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				StabilizationWindowSeconds: &invalidValue,
			},
		}
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
		testPolicy := autoscalingv2.MaxChangePolicySelect
		vas.Spec.CustomAutoscaler.Hpa.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
			ScaleDown: &autoscalingv2.HPAScalingRules{
				SelectPolicy: &testPolicy,
			},
		}
		vas.Default()
		_, err = vas.ValidateCreate()
		Expect(err).Should(Succeed())
		Expect(*vas.Spec.CustomAutoscaler.Hpa.Behavior.ScaleDown.StabilizationWindowSeconds).Should(Equal(int32(0)))
	})
})
