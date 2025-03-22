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

package v1beta1

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
)

var _ = Describe("verticaautoscaler_conversion", func() {
	It("should convert VerticaAutoscaler Spec from v1beta1 to v1", func() {
		v1beta1VAS := MakeVASWithMetrics() // function to generate test data for v1beta1
		v1VAS := v1.VerticaAutoscaler{}    // v1 version of VerticaAutoscaler

		// Set up a test scenario with v1beta1 data
		v1beta1VAS.Spec.VerticaDBName = "test-db"
		v1beta1VAS.Spec.ServiceName = "test-service"
		v1beta1VAS.Spec.TargetSize = 5
		v1beta1VAS.Spec.ScalingGranularity = ScalingGranularityType("node")

		// Perform conversion from v1beta1 to v1
		Ω(v1beta1VAS.ConvertTo(&v1VAS)).Should(Succeed())

		// Validate the conversion from v1beta1 to v1
		Ω(v1VAS.Spec.VerticaDBName).Should(Equal("test-db"))
		Ω(v1VAS.Spec.ServiceName).Should(Equal("test-service"))
		Ω(v1VAS.Spec.TargetSize).Should(Equal(int32(5)))
		Ω(v1VAS.Spec.ScalingGranularity).Should(Equal(v1.ScalingGranularityType("node")))

		// v1 -> v1beta1 conversion
		v1VAS.Spec.VerticaDBName = "new-db"
		v1VAS.Spec.ServiceName = "new-service"
		v1VAS.Spec.TargetSize = 10
		v1VAS.Spec.ScalingGranularity = v1.ScalingGranularityType("column")

		// Perform conversion from v1 to v1beta1
		Ω(v1beta1VAS.ConvertFrom(&v1VAS)).Should(Succeed())
		Ω(v1beta1VAS.Spec.VerticaDBName).Should(Equal("new-db"))
		Ω(v1beta1VAS.Spec.ServiceName).Should(Equal("new-service"))
		Ω(v1beta1VAS.Spec.TargetSize).Should(Equal(int32(10)))
		Ω(v1beta1VAS.Spec.ScalingGranularity).Should(Equal(ScalingGranularityType("column")))
	})

	It("should convert HPASpec from v1beta1 to v1", func() {
		v1beta1VAS := MakeVASWithMetrics() // function to generate test data for v1beta1
		v1VAS := v1.VerticaAutoscaler{}    // v1 version of VerticaAutoscaler

		// Perform conversion from v1beta1 to v1
		Ω(v1beta1VAS.ConvertTo(&v1VAS)).Should(Succeed())

		// Validate the conversion from v1beta1 to v1
		Ω(*v1VAS.Spec.CustomAutoscaler.Hpa.MinReplicas).Should(Equal(int32(3)))
		Ω(v1VAS.Spec.CustomAutoscaler.Hpa.MaxReplicas).Should(Equal(int32(6)))
		Ω(v1VAS.Spec.CustomAutoscaler.Hpa.Metrics[0].Metric.Type).Should(Equal(autoscalingv2.ResourceMetricSourceType))
		Ω(v1VAS.Spec.CustomAutoscaler.Hpa.Metrics[0].Metric.Resource.Name).Should(Equal(corev1.ResourceCPU))

		// v1 -> v1beta1 conversion
		minRep := int32(1)
		maxRep := int32(10)
		v1VAS.Spec.CustomAutoscaler.Hpa.MinReplicas = &minRep
		v1VAS.Spec.CustomAutoscaler.Hpa.MaxReplicas = maxRep
		v1VAS.Spec.CustomAutoscaler.Hpa.Metrics[0].Metric.Resource.Name = corev1.ResourceMemory

		// Perform conversion from v1 to v1beta1
		Ω(v1beta1VAS.ConvertFrom(&v1VAS)).Should(Succeed())
		Ω(*v1beta1VAS.Spec.CustomAutoscaler.Hpa.MinReplicas).Should(Equal(minRep))
		Ω(v1beta1VAS.Spec.CustomAutoscaler.Hpa.MaxReplicas).Should(Equal(maxRep))
		Ω(v1beta1VAS.Spec.CustomAutoscaler.Hpa.Metrics[0].Metric.Resource.Name).Should(Equal(corev1.ResourceMemory))
	})

	It("should convert ScaledObjectSpec from v1beta1 to v1", func() {
		v1beta1VAS := MakeVASWithScaledObject() // function to generate test data for v1beta1
		v1VAS := v1.VerticaAutoscaler{}         // v1 version of VerticaAutoscaler

		// Set up a test scenario with v1beta1 data
		pi := int32(10)
		cp := int32(30)
		v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.PollingInterval = &pi
		v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.CooldownPeriod = &cp

		// Perform conversion from v1beta1 to v1
		Ω(v1beta1VAS.ConvertTo(&v1VAS)).Should(Succeed())

		// Validate the conversion from v1beta1 to v1
		Ω(*v1VAS.Spec.CustomAutoscaler.ScaledObject.MinReplicas).Should(Equal(int32(3)))
		Ω(*v1VAS.Spec.CustomAutoscaler.ScaledObject.MaxReplicas).Should(Equal(int32(6)))
		Ω(*v1VAS.Spec.CustomAutoscaler.ScaledObject.PollingInterval).Should(Equal(pi))
		Ω(*v1VAS.Spec.CustomAutoscaler.ScaledObject.CooldownPeriod).Should(Equal(cp))

		// v1 -> v1beta1 conversion
		minRep := int32(1)
		maxRep := int32(10)
		pi = int32(20)
		cp = int32(40)
		v1VAS.Spec.CustomAutoscaler.ScaledObject.MinReplicas = &minRep
		v1VAS.Spec.CustomAutoscaler.ScaledObject.MaxReplicas = &maxRep
		v1VAS.Spec.CustomAutoscaler.ScaledObject.PollingInterval = &pi
		v1VAS.Spec.CustomAutoscaler.ScaledObject.CooldownPeriod = &cp

		// Perform conversion from v1 to v1beta1
		Ω(v1beta1VAS.ConvertFrom(&v1VAS)).Should(Succeed())
		Ω(*v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.MinReplicas).Should(Equal(minRep))
		Ω(*v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.MaxReplicas).Should(Equal(maxRep))
		Ω(*v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.PollingInterval).Should(Equal(pi))
		Ω(*v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.CooldownPeriod).Should(Equal(cp))
	})

	It("should convert PrometheusSpec from v1beta1 to v1 with useCachedMetrics", func() {
		v1beta1VAS := MakeVASWithScaledObject() // function to generate test data for v1beta1
		v1VAS := v1.VerticaAutoscaler{}         // v1 version of VerticaAutoscaler

		v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.Metrics = []ScaleTrigger{
			{
				Prometheus: &PrometheusSpec{
					ServerAddress:    "http://prometheus.local",
					Query:            "cpu_usage",
					Threshold:        80,
					UseCachedMetrics: true, // Test use of cached metrics
				},
			},
		}

		// Perform conversion from v1beta1 to v1
		Ω(v1beta1VAS.ConvertTo(&v1VAS)).Should(Succeed())

		// Validate the conversion from v1beta1 to v1
		Ω(v1VAS.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.UseCachedMetrics).Should(BeTrue())
		Ω(v1VAS.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.ServerAddress).Should(Equal("http://prometheus.local"))

		// v1 -> v1beta1 conversion
		v1VAS.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.UseCachedMetrics = false

		// Perform conversion from v1 to v1beta1
		Ω(v1beta1VAS.ConvertFrom(&v1VAS)).Should(Succeed())
		Ω(v1beta1VAS.Spec.CustomAutoscaler.ScaledObject.Metrics[0].Prometheus.UseCachedMetrics).Should(BeFalse())
	})
})
