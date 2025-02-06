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
	verticaautoscalerlog.Info("ConvertTo", "GroupVersion", GroupVersion, "name", v.Name, "namespace", v.Namespace, "uid", v.UID)
	dst := dstRaw.(*v1.VerticaAutoscaler)
	dst.Name = v.Name
	dst.Namespace = v.Namespace
	dst.Annotations = v.Annotations
	dst.UID = v.UID
	dst.Labels = v.Labels
	dst.Spec = convertToVasSpec(&v.Spec)
	dst.Status = convertToVasStatus(&v.Status)
	return nil
}

// ConvertFrom will handle conversion from the Hub type (v1) to v1beta1.
func (v *VerticaAutoscaler) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1.VerticaAutoscaler)
	verticadblog.Info("ConvertFrom", "GroupVersion", GroupVersion, "name", src.Name, "namespace", src.Namespace, "uid", src.UID)
	v.Name = src.Name
	v.Namespace = src.Namespace
	v.Annotations = src.Annotations
	v.UID = src.UID
	v.Labels = src.Labels
	v.Spec = convertVasFromSpec(src)
	v.Status = convertVasFromStatus(&src.Status)
	return nil
}

// convertToVasSpec will convert to a v1 VerticaAutoscalerSpec from a v1beta1 version
func convertToVasSpec(src *VerticaAutoscalerSpec) v1.VerticaAutoscalerSpec {
	dst := v1.VerticaAutoscalerSpec{
		VerticaDBName:      src.VerticaDBName,
		ServiceName:        src.ServiceName,
		ScalingGranularity: v1.ScalingGranularityType(src.ScalingGranularity),
		Template:           convertToSubcluster(&src.Template),
		CustomAutoscaler:   convertToVasCustomAutoscaler(src.CustomAutoscaler),
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
	return dst
}

func convertToVasCustomAutoscaler(src *CustomAutoscalerSpec) *v1.CustomAutoscalerSpec {
	if src == nil {
		return nil
	}
	dst := &v1.CustomAutoscalerSpec{
		MinReplicas: ptrOrNil(src.MinReplicas),
		MaxReplicas: src.MaxReplicas,
		Metrics:     make([]v1.MetricDefinition, len(src.Metrics)),
		Behavior:    ptrOrNil(src.Behavior),
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

// convertVasFromSpec will convert from a v1 VerticaAutoscalerSpec to a v1beta1 version
func convertVasFromSpec(src *v1.VerticaAutoscaler) VerticaAutoscalerSpec {
	srcSpec := &src.Spec

	dst := VerticaAutoscalerSpec{
		VerticaDBName:      srcSpec.VerticaDBName,
		ScalingGranularity: ScalingGranularityType(srcSpec.ScalingGranularity),
		ServiceName:        srcSpec.ServiceName,
		Template:           convertFromSubcluster(&srcSpec.Template),
		TargetSize:         srcSpec.TargetSize,
	}
	if srcSpec.CustomAutoscaler != nil {
		dst.CustomAutoscaler = &CustomAutoscalerSpec{
			MinReplicas: srcSpec.CustomAutoscaler.MinReplicas,
			MaxReplicas: srcSpec.CustomAutoscaler.MaxReplicas,
			Metrics:     make([]MetricDefinition, len(srcSpec.CustomAutoscaler.Metrics)),
			Behavior:    srcSpec.CustomAutoscaler.Behavior,
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
	return dst
}
