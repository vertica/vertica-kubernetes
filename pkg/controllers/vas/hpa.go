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
	autoscalingv2 "k8s.io/api/autoscaling/v2"
)

const (
	errorCmpResult = iota - 2
	lowerThanCmpResult
	equalToCmpResult
	greaterThanCmpResult
)

type metricStatus struct {
	name   string
	status autoscalingv2.MetricValueStatus
}

// getCurrentMetricStatus extracts and returns a metric's status from the
// hpa's status.
func getCurrentMetricStatus(m *autoscalingv2.MetricStatus) *metricStatus {
	ms := &metricStatus{}
	switch m.Type {
	case autoscalingv2.PodsMetricSourceType:
		ms.name = m.Pods.Metric.Name
		ms.status = m.Pods.Current
	case autoscalingv2.ObjectMetricSourceType:
		ms.name = m.Object.Metric.Name
		ms.status = m.Object.Current
	case autoscalingv2.ExternalMetricSourceType:
		ms.name = m.External.Metric.Name
		ms.status = m.External.Current
	case autoscalingv2.ResourceMetricSourceType:
		ms.name = m.Resource.Name.String()
		ms.status = m.Resource.Current
	case autoscalingv2.ContainerResourceMetricSourceType:
		ms.name = m.ContainerResource.Name.String()
		ms.status = m.ContainerResource.Current
	default:
		return nil
	}
	return ms
}

// cmp returns 0 if the metric's value is equal to the target,
// -1 if it is less than the target, or 1 if it is greater than the target.
func (ms *metricStatus) cmp(mt *autoscalingv2.MetricTarget) int {
	if ms.status.AverageUtilization != nil {
		if mt.AverageUtilization != nil {
			return ms.cmpAverageUtilization(*mt.AverageUtilization)
		}
	}
	if ms.status.Value != nil {
		if mt.Value != nil {
			return ms.status.Value.Cmp(*mt.Value)
		}
	}
	if ms.status.AverageValue != nil {
		if mt.AverageValue != nil {
			return ms.status.AverageValue.Cmp(*mt.AverageValue)
		}
	}
	return errorCmpResult
}

func (ms *metricStatus) cmpAverageUtilization(au int32) int {
	if *ms.status.AverageUtilization > au {
		return greaterThanCmpResult
	} else if *ms.status.AverageUtilization == au {
		return equalToCmpResult
	}
	return lowerThanCmpResult
}
