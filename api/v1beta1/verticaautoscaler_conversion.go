/*
Copyright [2021-2024] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
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
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// log is for logging in this package.
var verticaautoscalerlog = logf.Log.WithName("verticaautoscaler-resource")

// ConvertTo is a function to convert a v1beta1 CR to the v1 version of the CR.
func (v *VerticaAutoscaler) ConvertTo(dstRaw conversion.Hub) error {
	verticaautoscalerlog.Info("ConvertToVas", "GroupVersion", GroupVersion, "name", v.Name, "namespace", v.Namespace, "uid", v.UID)
	dst := dstRaw.(*v1.VerticaAutoscaler)
	dst.Name = v.Name
	dst.Namespace = v.Namespace
	dst.Annotations = v.Annotations
	dst.UID = v.UID
	dst.Labels = v.Labels
	dst.Spec = convertVasToSpec(&v.Spec)
	dst.Status = convertToVasStatus(&v.Status)
	return nil
}

// ConvertFrom will handle conversion from the Hub type (v1) to v1beta1.
func (v *VerticaAutoscaler) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.VerticaAutoscaler)
	verticadblog.Info("ConvertFromVas", "GroupVersion", GroupVersion, "name", src.Name, "namespace", src.Namespace, "uid", src.UID)
	v.Name = src.Name
	v.Namespace = src.Namespace
	v.Annotations = src.Annotations
	v.UID = src.UID
	v.Labels = src.Labels
	v.Spec = convertVasFromSpec(src)
	v.Status = convertVasFromStatus(&src.Status)
	return nil
}

// convertVasToSpec will convert to a v1 VerticaAutoscalerSpec from a v1beta1 version
func convertVasToSpec(src *VerticaAutoscalerSpec) v1.VerticaAutoscalerSpec {
	dst := v1.VerticaAutoscalerSpec{
		VerticaDBName:      src.VerticaDBName,
		ServiceName:        src.ServiceName,
		ScalingGranularity: v1.ScalingGranularityType(src.ScalingGranularity),
		Template:           convertToSubcluster(&src.Template),
		TargetSize:         src.TargetSize,
		CustomAutoscaler:   convertVasToCustomAutoscaler(src.CustomAutoscaler),
	}
	return dst
}

// convertVasFromSpec will convert from a v1 VerticaAutoscalerSpec to a v1beta1 version
func convertVasFromSpec(src *v1.VerticaAutoscaler) VerticaAutoscalerSpec {
	srcSpec := &src.Spec

	dst := VerticaAutoscalerSpec{
		VerticaDBName:      srcSpec.VerticaDBName,
		ServiceName:        srcSpec.ServiceName,
		ScalingGranularity: ScalingGranularityType(srcSpec.ScalingGranularity),
		Template:           convertFromSubcluster(&srcSpec.Template),
		TargetSize:         srcSpec.TargetSize,
	}
	if srcSpec.CustomAutoscaler != nil {
		dst.CustomAutoscaler = &CustomAutoscalerSpec{
			Type: srcSpec.CustomAutoscaler.Type,
		}
		if srcSpec.CustomAutoscaler.Hpa != nil {
			dst.CustomAutoscaler.Hpa = convertVasFromHPASpec(srcSpec.CustomAutoscaler.Hpa)
		}
		if srcSpec.CustomAutoscaler.ScaledObject != nil {
			dst.CustomAutoscaler.ScaledObject = convertVasFromScaledObjectSpec(srcSpec.CustomAutoscaler.ScaledObject)
		}
	}
	return dst
}

// convertVasFromHPASpec will convert from a v1 HPASpec to a v1beta1 version
func convertVasFromHPASpec(src *v1.HPASpec) *HPASpec {
	dst := &HPASpec{
		MinReplicas: src.MinReplicas,
		MaxReplicas: src.MaxReplicas,
		Metrics:     make([]MetricDefinition, len(src.Metrics)),
		Behavior:    src.Behavior,
	}
	for i := range src.Metrics {
		srcMetric := &src.Metrics[i]
		dst.Metrics[i] = MetricDefinition{
			ThresholdAdjustmentValue: srcMetric.ThresholdAdjustmentValue,
			Metric:                   srcMetric.Metric,
			ScaleDownThreshold:       ptrOrNil(srcMetric.ScaleDownThreshold),
		}
	}
	return dst
}

// convertVasFromScaledObjectSpec will convert from a v1 ScaledObjectSpec to a v1beta1 version
func convertVasFromScaledObjectSpec(src *v1.ScaledObjectSpec) *ScaledObjectSpec {
	dst := &ScaledObjectSpec{
		MinReplicas:     src.MinReplicas,
		MaxReplicas:     src.MaxReplicas,
		PollingInterval: src.PollingInterval,
		CooldownPeriod:  src.CooldownPeriod,
		Metrics:         make([]ScaleTrigger, len(src.Metrics)),
		Behavior:        src.Behavior,
	}
	for i := range src.Metrics {
		srcMetric := &src.Metrics[i]
		dst.Metrics[i] = ScaleTrigger{
			Type:       TriggerType(srcMetric.Type),
			Name:       srcMetric.Name,
			AuthSecret: srcMetric.AuthSecret,
			MetricType: srcMetric.MetricType,
		}
		if srcMetric.Prometheus != nil {
			dst.Metrics[i].Prometheus = &PrometheusSpec{
				ServerAddress:      srcMetric.Prometheus.ServerAddress,
				Query:              srcMetric.Prometheus.Query,
				Threshold:          srcMetric.Prometheus.Threshold,
				ScaleDownThreshold: srcMetric.Prometheus.ScaleDownThreshold,
			}
		}
		if srcMetric.Resource != nil {
			dst.Metrics[i].Resource = &CPUMemorySpec{
				Threshold: srcMetric.Resource.Threshold,
			}
		}
	}
	return dst
}

// convertToVasStatus will convert to a v1 VerticaAutoscalerStatus from a v1beta1 version
func convertToVasStatus(src *VerticaAutoscalerStatus) v1.VerticaAutoscalerStatus {
	dst := v1.VerticaAutoscalerStatus{
		ScalingCount: src.ScalingCount,
		CurrentSize:  src.CurrentSize,
		Selector:     src.Selector,
		Conditions:   make([]v1.VerticaAutoscalerCondition, len(src.Conditions)),
	}
	for i := range src.Conditions {
		srcMetric := &src.Conditions[i]
		dst.Conditions[i] = v1.VerticaAutoscalerCondition{
			Type:               v1.VerticaAutoscalerConditionType(srcMetric.Type),
			Status:             srcMetric.Status,
			LastTransitionTime: srcMetric.LastTransitionTime,
		}
	}
	return dst
}

// convertVasToCustomAutoscaler will convert a v1beta1 CustomAutoscalerSpec to v1 version
func convertVasToCustomAutoscaler(src *CustomAutoscalerSpec) *v1.CustomAutoscalerSpec {
	if src == nil {
		return nil
	}
	dst := &v1.CustomAutoscalerSpec{
		Type: src.Type,
	}
	if src.Hpa != nil {
		dst.Hpa = convertVasToHPASpec(src.Hpa)
	}
	if src.ScaledObject != nil {
		dst.ScaledObject = convertVasToScaledObjectSpec(src.ScaledObject)
	}
	return dst
}

// convertVasToHPASpec will convert a v1beta1 HPASpec to v1 version
func convertVasToHPASpec(src *HPASpec) *v1.HPASpec {
	dst := &v1.HPASpec{
		MinReplicas: src.MinReplicas,
		MaxReplicas: src.MaxReplicas,
		Metrics:     make([]v1.MetricDefinition, len(src.Metrics)),
		Behavior:    src.Behavior,
	}
	for i := range src.Metrics {
		srcMetric := &src.Metrics[i]
		dst.Metrics[i] = v1.MetricDefinition{
			ThresholdAdjustmentValue: srcMetric.ThresholdAdjustmentValue,
			Metric:                   srcMetric.Metric,
			ScaleDownThreshold:       ptrOrNil(srcMetric.ScaleDownThreshold),
		}
	}
	return dst
}

// convertVasToScaledObjectSpec will convert a v1beta1 ScaledObjectSpec to v1 version
func convertVasToScaledObjectSpec(src *ScaledObjectSpec) *v1.ScaledObjectSpec {
	dst := &v1.ScaledObjectSpec{
		MinReplicas:     src.MinReplicas,
		MaxReplicas:     src.MaxReplicas,
		PollingInterval: src.PollingInterval,
		CooldownPeriod:  src.CooldownPeriod,
		Metrics:         make([]v1.ScaleTrigger, len(src.Metrics)),
		Behavior:        src.Behavior,
	}
	for i := range src.Metrics {
		srcMetric := &src.Metrics[i]
		dst.Metrics[i] = v1.ScaleTrigger{
			Type:       v1.TriggerType(srcMetric.Type),
			Name:       srcMetric.Name,
			AuthSecret: srcMetric.AuthSecret,
			MetricType: srcMetric.MetricType,
		}
		if srcMetric.Prometheus != nil {
			dst.Metrics[i].Prometheus = &v1.PrometheusSpec{
				ServerAddress:      srcMetric.Prometheus.ServerAddress,
				Query:              srcMetric.Prometheus.Query,
				Threshold:          srcMetric.Prometheus.Threshold,
				ScaleDownThreshold: srcMetric.Prometheus.ScaleDownThreshold,
			}
		}
		if srcMetric.Resource != nil {
			dst.Metrics[i].Resource = &v1.CPUMemorySpec{
				Threshold: srcMetric.Resource.Threshold,
			}
		}
	}
	return dst
}

// convertVasFromStatus will convert from a v1 VerticaAutoscalerStatus to a v1beta1 version
func convertVasFromStatus(src *v1.VerticaAutoscalerStatus) VerticaAutoscalerStatus {
	dst := VerticaAutoscalerStatus{
		ScalingCount: src.ScalingCount,
		CurrentSize:  src.CurrentSize,
		Selector:     src.Selector,
		Conditions:   make([]VerticaAutoscalerCondition, len(src.Conditions)),
	}
	for i := range src.Conditions {
		srcMetric := &src.Conditions[i]
		dst.Conditions[i] = VerticaAutoscalerCondition{
			Type:               VerticaAutoscalerConditionType(srcMetric.Type),
			Status:             srcMetric.Status,
			LastTransitionTime: srcMetric.LastTransitionTime,
		}
	}
	return dst
}
