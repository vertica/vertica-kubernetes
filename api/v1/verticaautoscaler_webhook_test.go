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

	It("should fail if the service name differs", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = SubclusterScalingGranularity
		vas.Spec.Template.ServiceName = "somethingelse"
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
		vas.Spec.Template.ServiceName = "somethingelse"
		vas.Spec.ScalingGranularity = PodScalingGranularity
		_, err4 := vas.ValidateUpdate(MakeVAS())
		Expect(err4).ShouldNot(Succeed())
	})

	It("should not have invalid subcluster service name", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = SubclusterScalingGranularity
		vas.Spec.Template.ServiceName = "sc_svc"
		vas.Spec.Template.Size = 1
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should not have invalid subcluster name", func() {
		vas := MakeVAS()
		vas.Spec.ScalingGranularity = SubclusterScalingGranularity
		vas.Spec.Template.Name = "Sub_"
		vas.Spec.Template.Size = 1
		_, err := vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
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

	It("should fail if scaledobject metrics type is not set properly", func() {
		vas := MakeVASWithScaledObject()
		_, err := vas.ValidateCreate()
		Expect(err).Should(Succeed())
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = "BadValue"
		_, err = vas.ValidateCreate()
		Expect(err).ShouldNot(Succeed())
	})

	It("should fail if scaledobject metrics type is prometheus and metricType is not set properly", func() {
		vas := MakeVASWithScaledObject()
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Type = PrometheusTriggerType
		vas.Spec.CustomAutoscaler.ScaledObject.Metrics[0].MetricType = autoscalingv2.ValueMetricType
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
})
