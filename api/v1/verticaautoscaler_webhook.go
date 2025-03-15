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

//nolint:lll
package v1

import (
	"fmt"
	"slices"

	autoscalingv2 "k8s.io/api/autoscaling/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	defaultStabilizationWindowSeconds = 0
)

// log is for logging in this package.
var verticaautoscalerlog = logf.Log.WithName("verticaautoscaler-resource")

// +kubebuilder:webhook:path=/mutate-vertica-com-v1-verticaautoscaler,mutating=true,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticaautoscalers,verbs=create;update,versions=v1,name=mverticaautoscaler.v1.kb.io,admissionReviewVersions=v1

func (v *VerticaAutoscaler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(v).
		Complete()
}

var _ webhook.Defaulter = &VerticaAutoscaler{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (v *VerticaAutoscaler) Default() {
	verticaautoscalerlog.Info("default", "name", v.Name, "GroupVersion", GroupVersion)

	if v.Spec.Template.Type == "" {
		v.Spec.Template.Type = v.Spec.Template.GetType()
	}
	if v.HasScaleInThreshold() {
		defaultV := int32(defaultStabilizationWindowSeconds)
		if v.Spec.CustomAutoscaler.Hpa.Behavior == nil {
			v.Spec.CustomAutoscaler.Hpa.Behavior = &autoscalingv2.HorizontalPodAutoscalerBehavior{
				ScaleDown: &autoscalingv2.HPAScalingRules{},
			}
		}
		if v.Spec.CustomAutoscaler.Hpa.Behavior.ScaleDown == nil {
			v.Spec.CustomAutoscaler.Hpa.Behavior.ScaleDown = &autoscalingv2.HPAScalingRules{}
		}
		if v.Spec.CustomAutoscaler.Hpa.Behavior.ScaleDown.StabilizationWindowSeconds == nil {
			v.Spec.CustomAutoscaler.Hpa.Behavior.ScaleDown.StabilizationWindowSeconds = &defaultV
		}
	}
}

var _ webhook.Validator = &VerticaAutoscaler{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateCreate() (admission.Warnings, error) {
	verticaautoscalerlog.Info("validate create", "name", v.Name)

	allErrs := v.validateSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaAutoscalerKind}, v.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	verticaautoscalerlog.Info("validate update", "name", v.Name)
	allErrs := append(v.validateImmutableFields(old), v.validateSpec()...)

	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaAutoscalerKind}, v.Name, allErrs)
}

func (v *VerticaAutoscaler) validateImmutableFields(old runtime.Object) field.ErrorList {
	var allErrs field.ErrorList
	oldObj := old.(*VerticaAutoscaler)

	allErrs = v.checkImmutableCustomAutoscaler(oldObj, allErrs)
	return allErrs
}

func (v *VerticaAutoscaler) checkImmutableCustomAutoscaler(oldObj *VerticaAutoscaler, allErrs field.ErrorList) field.ErrorList {
	// cannot set customAutoscaler after CR creation
	if !oldObj.IsCustomAutoScalerSet() && v.IsCustomAutoScalerSet() {
		err := field.Invalid(field.NewPath("spec").Child("customAutoscaler"),
			v.Spec.CustomAutoscaler,
			"cannot set customAutoscaler after CR creation.")
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateDelete() (admission.Warnings, error) {
	verticaautoscalerlog.Info("validate delete", "name", v.Name)

	return nil, nil
}

// validateSpec will validate the current VerticaAutoscaler to see if it is valid
func (v *VerticaAutoscaler) validateSpec() field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = v.validateScalingGranularity(allErrs)
	allErrs = v.validateSubclusterTemplate(allErrs)
	allErrs = v.validateCustomAutoscaler(allErrs)
	allErrs = v.validateScaledObject(allErrs)
	allErrs = v.validateScaledObjectMetric(allErrs)
	allErrs = v.validateHPA(allErrs)
	allErrs = v.validatePrometheusAuthModes(allErrs)
	allErrs = v.validateScaleInThreshold(allErrs)
	allErrs = v.validateHPAReplicas(allErrs)
	allErrs = v.validateScaledObjectReplicas(allErrs)
	allErrs = v.validateMetricsName(allErrs)
	return allErrs
}

// validateScalingGranularity will check if the scalingGranularity field is valid
func (v *VerticaAutoscaler) validateScalingGranularity(allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec").Child("scalingGranularity")
	switch v.Spec.ScalingGranularity {
	case PodScalingGranularity:
		if v.Spec.ServiceName == "" {
			err := field.Invalid(pathPrefix,
				v.Spec.ScalingGranularity,
				fmt.Sprintf("Scaling granularity must be '%s' if service name is empty.",
					SubclusterScalingGranularity))
			allErrs = append(allErrs, err)
		}
		return allErrs
	case SubclusterScalingGranularity:
		return allErrs
	default:
		err := field.Invalid(pathPrefix,
			v.Spec.ScalingGranularity,
			fmt.Sprintf("scalingGranularity must be set to either %s or %s",
				SubclusterScalingGranularity,
				PodScalingGranularity))
		return append(allErrs, err)
	}
}

// validateSubclusterTemplate will validate the subcluster template
func (v *VerticaAutoscaler) validateSubclusterTemplate(allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec").Child("template")
	// Validate template name
	if v.CanUseTemplate() && v.Spec.Template.Name != "" && !IsValidSubclusterName(GenCompatibleFQDNHelper(v.Spec.Template.Name)) {
		err := field.Invalid(pathPrefix.Child("name"),
			v.Spec.Template.Name,
			fmt.Sprintf("The name in the subcluster template must match regex '%s'",
				RFC1123DNSSubdomainNameRegex),
		)
		allErrs = append(allErrs, err)
	}
	// Validate each template service name
	if v.CanUseTemplate() && v.Spec.Template.ServiceName != "" && !IsValidServiceName(v.Spec.Template.ServiceName) {
		err := field.Invalid(pathPrefix.Child("serviceName"),
			v.Spec.Template.ServiceName,
			fmt.Sprintf("The serviceName in the subcluster template must match regex '%s'",
				RFC1035DNSLabelNameRegex),
		)
		allErrs = append(allErrs, err)
	}
	if v.CanUseTemplate() && v.Spec.ServiceName != "" && v.Spec.Template.ServiceName != v.Spec.ServiceName {
		err := field.Invalid(pathPrefix.Child("serviceName"),
			v.Spec.Template.ServiceName,
			"The serviceName in the subcluster template must match spec.serviceName")
		allErrs = append(allErrs, err)
	}
	if v.CanUseTemplate() && v.Spec.ScalingGranularity == PodScalingGranularity {
		err := field.Invalid(pathPrefix.Child("serviceName"),
			v.Spec.Template.ServiceName,
			"You cannot use the template if scalingGranularity is Pod.  Set the template size to 0 to disable the template")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// validateCustomAutoscaler will check if the CustomAutoscaler field is valid
func (v *VerticaAutoscaler) validateCustomAutoscaler(allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec").Child("customAutoscaler")
	validTypes := []string{HPA, ScaledObject, ""}
	// validate type
	if v.Spec.CustomAutoscaler != nil && !slices.Contains(validTypes, v.Spec.CustomAutoscaler.Type) {
		err := field.Invalid(pathPrefix.Child("type"),
			v.Spec.CustomAutoscaler.Type,
			fmt.Sprintf("Type must be one of '%s', '%s' or empty.",
				HPA, ScaledObject),
		)
		allErrs = append(allErrs, err)
	}

	// customAutoscaler.Hpa must be set if customAutoscaler.type is HPA
	if v.IsHpaType() && v.Spec.CustomAutoscaler.Hpa == nil {
		err := field.Invalid(pathPrefix.Child("type"),
			v.Spec.CustomAutoscaler.Type,
			fmt.Sprintf("customAutoscaler.Hpa must be set if customAutoscaler.type is %s.", HPA),
		)
		allErrs = append(allErrs, err)
	}

	// customAutoscaler.ScaledObject must be set if customAutoscaler.type is ScaledObject
	if v.IsScaledObjectType() && v.Spec.CustomAutoscaler.ScaledObject == nil {
		err := field.Invalid(pathPrefix.Child("type"),
			v.Spec.CustomAutoscaler.Type,
			fmt.Sprintf("customAutoscaler.ScaledObject must be set if customAutoscaler.type is %s or empty.", ScaledObject),
		)
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// validateHPAReplicas will check if HPA minReplicas and maxReplicas are valid
func (v *VerticaAutoscaler) validateHPAReplicas(allErrs field.ErrorList) field.ErrorList {
	if v.IsHpaEnabled() {
		pathPrefix := field.NewPath("spec").Child("customAutoscaler").Child("HPA")
		if v.Spec.CustomAutoscaler.Hpa.MaxReplicas == 0 {
			err := field.Invalid(pathPrefix.Child("MaxReplicas"),
				v.Spec.CustomAutoscaler.Hpa.MaxReplicas,
				"maxReplicas must be set.")
			allErrs = append(allErrs, err)
		}

		if v.Spec.CustomAutoscaler.Hpa.MinReplicas != nil &&
			v.Spec.CustomAutoscaler.Hpa.MaxReplicas < *v.Spec.CustomAutoscaler.Hpa.MinReplicas {
			err := field.Invalid(pathPrefix.Child("MaxReplicas"),
				v.Spec.CustomAutoscaler.Hpa.MaxReplicas,
				fmt.Sprintf("maxReplicas %d cannot be less than minReplicas %d.",
					v.Spec.CustomAutoscaler.Hpa.MaxReplicas, *v.Spec.CustomAutoscaler.Hpa.MinReplicas),
			)
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// validateScaledObjectReplicas will check if ScaledObject minReplicas and maxReplicas are valid
func (v *VerticaAutoscaler) validateScaledObjectReplicas(allErrs field.ErrorList) field.ErrorList {
	if v.IsScaledObjectEnabled() {
		pathPrefix := field.NewPath("spec").Child("customAutoscaler").Child("ScaledObject")

		if v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas == nil {
			err := field.Invalid(pathPrefix.Child("MaxReplicas"),
				v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas,
				"maxReplicas must be set.")
			allErrs = append(allErrs, err)
		}

		if v.Spec.CustomAutoscaler.ScaledObject.MinReplicas != nil && v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas != nil &&
			*v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas < *v.Spec.CustomAutoscaler.ScaledObject.MinReplicas {
			err := field.Invalid(pathPrefix.Child("MaxReplicas"),
				v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas,
				fmt.Sprintf("maxReplicas %d cannot be less than minReplicas %d.",
					*v.Spec.CustomAutoscaler.ScaledObject.MaxReplicas, *v.Spec.CustomAutoscaler.ScaledObject.MinReplicas),
			)
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

// validateMetricsName makes sure 2 metrics cannot have the same name
func (v *VerticaAutoscaler) validateMetricsName(allErrs field.ErrorList) field.ErrorList {
	metricsSet := map[string]struct{}{}
	if v.IsScaledObjectEnabled() {
		pathPrefix := field.NewPath("spec").Child("customAutoscaler")
		for i, metric := range v.Spec.CustomAutoscaler.ScaledObject.Metrics {
			path := pathPrefix.Child("scaledObject").Child("metrics").Index(i).Child("name")

			// Check for duplicate metric names directly
			if _, exists := metricsSet[metric.Name]; exists {
				err := field.Invalid(path, metric.Name, fmt.Sprintf("Metric name '%s' cannot be the same.", metric.Name))
				allErrs = append(allErrs, err)
				continue
			}

			// Store the metric name in the set
			metricsSet[metric.Name] = struct{}{}
		}
	}
	return allErrs
}

// validateScaledObject will check if the ScaledObject field is valid
func (v *VerticaAutoscaler) validateScaledObject(allErrs field.ErrorList) field.ErrorList {
	validTriggers := []TriggerType{CPUTriggerType, MemTriggerType, PrometheusTriggerType, ""}
	prometheusMetricTypes := []autoscalingv2.MetricTargetType{autoscalingv2.ValueMetricType, autoscalingv2.AverageValueMetricType}
	cpumemMetricTypes := []autoscalingv2.MetricTargetType{autoscalingv2.UtilizationMetricType, autoscalingv2.AverageValueMetricType}
	pathPrefix := field.NewPath("spec").Child("customAutoscaler")
	if v.IsScaledObjectEnabled() {
		metricsPathPrefix := pathPrefix.Child("scaledObject").Child("metrics")
		if len(v.Spec.CustomAutoscaler.ScaledObject.Metrics) == 0 {
			err := field.Invalid(metricsPathPrefix,
				v.Spec.CustomAutoscaler.ScaledObject.Metrics,
				"metrics must contain at least one element.",
			)
			allErrs = append(allErrs, err)
			return allErrs
		}
		metrics := v.Spec.CustomAutoscaler.ScaledObject.Metrics
		for i := range metrics {
			metric := &metrics[i]
			// validate metric type
			if !slices.Contains(validTriggers, metric.Type) {
				err := field.Invalid(metricsPathPrefix.Index(i).Child("type"),
					metric.Type,
					fmt.Sprintf("Type must be one of '%s', '%s', '%s' or empty.",
						CPUTriggerType, MemTriggerType, PrometheusTriggerType),
				)
				allErrs = append(allErrs, err)
			}
			// validate prometheus type metric
			if metric.Type == PrometheusTriggerType && !slices.Contains(prometheusMetricTypes, metric.MetricType) {
				err := field.Invalid(metricsPathPrefix.Index(i).Child("type"),
					metric.MetricType,
					fmt.Sprintf("When Type is set to %s "+
						"metricType must be one of '%s', '%s'.",
						PrometheusTriggerType, autoscalingv2.ValueMetricType, autoscalingv2.AverageValueMetricType),
				)
				allErrs = append(allErrs, err)
			}
			// validate cpu/mem type metric
			if (metric.Type == CPUTriggerType || metric.Type == MemTriggerType) && !slices.Contains(cpumemMetricTypes, metric.MetricType) {
				err := field.Invalid(metricsPathPrefix.Index(i).Child("type"),
					metric.MetricType,
					fmt.Sprintf("When Type is set to %s or %s "+
						"metricType must be one of '%s', '%s'.",
						CPUTriggerType, MemTriggerType, autoscalingv2.UtilizationMetricType, autoscalingv2.AverageValueMetricType),
				)
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

func (v *VerticaAutoscaler) validatePrometheusAuthModes(allErrs field.ErrorList) field.ErrorList {
	authModeTypes := []PrometheusAuthModes{PrometheusAuthBasic, PrometheusAuthBearer, PrometheusAuthCustom, PrometheusAuthTLS, PrometheusAuthTLSAndBasic}
	pathPrefix := field.NewPath("spec").Child("customAutoscaler")
	if v.IsScaledObjectEnabled() {
		for i := range v.Spec.CustomAutoscaler.ScaledObject.Metrics {
			metric := &v.Spec.CustomAutoscaler.ScaledObject.Metrics[i]
			// validate if authSecert is set, AuthModes can not be empty
			if metric.Prometheus != nil && metric.AuthSecret != "" && metric.Prometheus.AuthModes == "" {
				err := field.Invalid(pathPrefix.Child("scaledObject").Child("metrics").Index(i).Child("prometheus").Child("authModes"),
					v.Spec.CustomAutoscaler.ScaledObject.Metrics[i].Prometheus.AuthModes,
					"When authSecret is set authModes can not be empty.",
				)
				allErrs = append(allErrs, err)
			}
			// validate AuthModes type if set
			if metric.Prometheus != nil && metric.AuthSecret != "" && metric.Prometheus.AuthModes != "" &&
				!slices.Contains(authModeTypes, metric.Prometheus.AuthModes) {
				err := field.Invalid(pathPrefix.Child("scaledObject").Child("metrics").Index(i).Child("prometheus").Child("authModes"),
					v.Spec.CustomAutoscaler.ScaledObject.Metrics[i].Prometheus.AuthModes,
					fmt.Sprintf("AuthModes type must be one of '%s', '%s' , '%s', '%s' or '%s'.",
						PrometheusAuthBasic, PrometheusAuthBearer, PrometheusAuthCustom, PrometheusAuthTLS, PrometheusAuthTLSAndBasic),
				)
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

// validateScaledObjectNil will check if the ScaledObject metric is nil
func (v *VerticaAutoscaler) validateScaledObjectMetric(allErrs field.ErrorList) field.ErrorList {
	if v.IsScaledObjectEnabled() {
		pathPrefix := field.NewPath("spec").Child("customAutoscaler")
		for i := range v.Spec.CustomAutoscaler.ScaledObject.Metrics {
			metric := &v.Spec.CustomAutoscaler.ScaledObject.Metrics[i]

			// metrics[].prometheus must be set if metrics[].type is "prometheus"
			if metric.Type == PrometheusTriggerType && metric.Prometheus == nil {
				err := field.Invalid(pathPrefix.Child("scaledObject").Child("metrics").Index(i).Child("type"),
					v.Spec.CustomAutoscaler.ScaledObject.Metrics[i].MetricType,
					fmt.Sprintf("When Type is set to %s, metrics[].prometheus must be set.",
						PrometheusTriggerType),
				)
				allErrs = append(allErrs, err)
			}

			// metrics[].resource must be set if metrics[].type is "cpu" or "memory"
			if (metric.Type == CPUTriggerType || metric.Type == MemTriggerType) && metric.Resource == nil {
				err := field.Invalid(pathPrefix.Child("scaledObject").Child("metrics").Index(i).Child("type"),
					v.Spec.CustomAutoscaler.ScaledObject.Metrics[i].MetricType,
					fmt.Sprintf("When Type is set to %s or %s, metrics[].resource must be set.",
						CPUTriggerType, MemTriggerType),
				)
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}

// validateHPA will check if the HPA field is valid
func (v *VerticaAutoscaler) validateHPA(allErrs field.ErrorList) field.ErrorList {
	if !v.IsHpaEnabled() {
		// Early return if HPA is not enabled
		return allErrs
	}
	pathPrefix := field.NewPath("spec", "customAutoscaler")
	hpa := v.Spec.CustomAutoscaler.Hpa

	// validate stabilization window
	if v.HasScaleInThreshold() && hpa.Behavior != nil &&
		hpa.Behavior.ScaleDown != nil && *hpa.Behavior.ScaleDown.StabilizationWindowSeconds != 0 {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("hpa", "behavior", "scaleDown", "stabilizationWindowSeconds"),
			hpa.Behavior.ScaleDown.StabilizationWindowSeconds,
			"When scaleInThreshold is set, scaleDown stabilization window must be 0",
		))
	}

	metricsPathPrefix := pathPrefix.Child("hpa", "metrics")
	if len(hpa.Metrics) == 0 {
		err := field.Invalid(metricsPathPrefix,
			hpa.Metrics,
			"metrics must contain at least one element.",
		)
		allErrs = append(allErrs, err)
		return allErrs
	}
	for i := range hpa.Metrics {
		metric := &hpa.Metrics[i]
		allErrs = v.validateMetricFields(metric, i, allErrs)
	}

	return allErrs
}

// Helper method to validate metric target and its fields, for each type, check if required fields have been setup.
func (v *VerticaAutoscaler) validateMetricFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	if metric == nil {
		return allErrs
	}
	switch metric.Metric.Type {
	case autoscalingv2.PodsMetricSourceType:
		allErrs = v.validateHPAMetricPodFields(metric, index, allErrs)
	case autoscalingv2.ObjectMetricSourceType:
		allErrs = v.validateHPAMetricObjectFields(metric, index, allErrs)
	case autoscalingv2.ContainerResourceMetricSourceType:
		allErrs = v.validateHPAMetricContainerFields(metric, index, allErrs)
	case autoscalingv2.ExternalMetricSourceType:
		allErrs = v.validateHPAMetricExternalFields(metric, index, allErrs)
	case autoscalingv2.ResourceMetricSourceType:
		allErrs = v.validateHPAMetricResourceFields(metric, index, allErrs)
	default:
		pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "type")
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			metric.Metric.Type,
			fmt.Sprintf("metric type must be one of %s, %s, %s, %s and %s",
				autoscalingv2.PodsMetricSourceType,
				autoscalingv2.ObjectMetricSourceType,
				autoscalingv2.ContainerResourceMetricSourceType,
				autoscalingv2.ExternalMetricSourceType,
				autoscalingv2.ResourceMetricSourceType,
			),
		))
	}
	return allErrs
}

// Helper method to validate HPA pod metric and target
func (v *VerticaAutoscaler) validateHPAMetricPodFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "pods")
	pods := metric.Metric.Pods

	// Validate metric pods target type
	if pods == nil {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			pods,
			fmt.Sprintf("required field 'pods' must be set when metric type is %s", autoscalingv2.PodsMetricSourceType),
		))
		return allErrs
	}

	allErrs = v.validateHPAMetricTarget(pods.Target, pathPrefix, allErrs)

	// Validate metric pods metric
	if pods.Metric.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("metric", "name"),
			pods.Metric.Name,
			"metric name must not be empty",
		))
	}

	return allErrs
}

// Helper method to validate HPA object metric, describedObject and target
func (v *VerticaAutoscaler) validateHPAMetricObjectFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "object")
	object := metric.Metric.Object

	// Validate metric object target type
	if object == nil {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			object,
			fmt.Sprintf("required field 'object' must be set when metric type is %s", autoscalingv2.ObjectMetricSourceType),
		))
		return allErrs
	}

	allErrs = v.validateHPAMetricTarget(object.Target, pathPrefix, allErrs)

	// Validate metric object DescribedObject
	if object.DescribedObject.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("describedObject", "name"),
			object.DescribedObject.Name,
			"describedObject name must not be empty",
		))
	}

	if object.DescribedObject.Kind == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("describedObject", "kind"),
			object.DescribedObject.Kind,
			"describedObject kind must not be empty",
		))
	}

	// Validate metric object metric
	if object.Metric.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("metric", "name"),
			object.Metric.Name,
			"metric name must not be empty",
		))
	}
	return allErrs
}

// Helper method to validate HPA container name, container and target
func (v *VerticaAutoscaler) validateHPAMetricContainerFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "containerResource")
	containerResource := metric.Metric.ContainerResource

	// Validate metric containerResource target type
	if containerResource == nil {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			containerResource,
			fmt.Sprintf("required field 'containerResource' must be set when metric type is %s", autoscalingv2.ContainerResourceMetricSourceType),
		))
		return allErrs
	}

	allErrs = v.validateHPAMetricTarget(containerResource.Target, pathPrefix, allErrs)

	// Validate metric containerResource name
	if containerResource.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("name"),
			containerResource.Name,
			"resource name must not be empty",
		))
	}

	// Validate metric containerResource container
	if containerResource.Container == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("container"),
			containerResource.Container,
			"container name must be set",
		))
	}

	return allErrs
}

// Helper method to validate HPA external mertric and target
func (v *VerticaAutoscaler) validateHPAMetricExternalFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "external")
	external := metric.Metric.External

	// Validate metric external target type
	if external == nil {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			external,
			fmt.Sprintf("required field 'external' must be set when metric type is %s", autoscalingv2.ExternalMetricSourceType),
		))
		return allErrs
	}

	allErrs = v.validateHPAMetricTarget(external.Target, pathPrefix, allErrs)

	// Validate metric external metric
	if external.Metric.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("metric", "name"),
			external.Metric,
			"external metric name must be set",
		))
	}

	return allErrs
}

// Helper method to validate HPA resource name and target
func (v *VerticaAutoscaler) validateHPAMetricResourceFields(metric *MetricDefinition, index int, allErrs field.ErrorList) field.ErrorList {
	pathPrefix := field.NewPath("spec", "customAutoscaler", "hpa", "metrics").Index(index).Child("metric", "resource")
	resource := metric.Metric.Resource

	// Validate metric resource target type
	if resource == nil {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix,
			resource,
			fmt.Sprintf("required field 'resource' must be set when metric type is %s", autoscalingv2.ResourceMetricSourceType),
		))
		return allErrs
	}

	allErrs = v.validateHPAMetricTarget(resource.Target, pathPrefix, allErrs)

	// Validate metric resource name
	if resource.Name == "" {
		allErrs = append(allErrs, field.Invalid(
			pathPrefix.Child("name"),
			resource.Name,
			"resource name must not be empty",
		))
	}

	return allErrs
}

// Helper method to validate HPA metric target value based on its type
func (v *VerticaAutoscaler) validateHPAMetricTarget(metric autoscalingv2.MetricTarget, pathPrefix *field.Path, allErrs field.ErrorList) field.ErrorList {
	targetPath := pathPrefix.Child("target")

	switch metric.Type {
	case autoscalingv2.UtilizationMetricType:
		if metric.AverageUtilization == nil {
			allErrs = append(allErrs, createInvalidError(
				targetPath.Child("averageUtilization"),
				metric.AverageUtilization,
				autoscalingv2.UtilizationMetricType,
				"averageUtilization",
			))
		}
	case autoscalingv2.ValueMetricType:
		if metric.Value == nil {
			allErrs = append(allErrs, createInvalidError(
				targetPath.Child("value"),
				metric.Value,
				autoscalingv2.ValueMetricType,
				"value",
			))
		}
	case autoscalingv2.AverageValueMetricType:
		if metric.AverageValue == nil {
			allErrs = append(allErrs, createInvalidError(
				targetPath.Child("averageValue"),
				metric.AverageValue,
				autoscalingv2.AverageValueMetricType,
				"averageValue",
			))
		}
	default:
		allErrs = append(allErrs, field.Invalid(
			targetPath.Child("type"),
			metric.Type,
			fmt.Sprintf("metric target type must be one of %s, %s, and %s",
				autoscalingv2.UtilizationMetricType,
				autoscalingv2.ValueMetricType,
				autoscalingv2.AverageValueMetricType,
			),
		))
	}
	return allErrs
}

func createInvalidError(path *field.Path, value interface{}, metricType autoscalingv2.MetricTargetType, fieldName string) *field.Error {
	return field.Invalid(
		path,
		value,
		fmt.Sprintf("HPA metric %s type missing required value: %s", metricType, fieldName),
	)
}

// validateScaleInThreshold will check if scaleInThreshold type matches the threshold used for scale out
func (v *VerticaAutoscaler) validateScaleInThreshold(allErrs field.ErrorList) field.ErrorList {
	// validate scaleInThreshold type
	if v.HasScaleInThreshold() {
		pathPrefix := field.NewPath("spec").Child("customAutoscaler").Child("hpa").Child("metrics")
		for i := range v.Spec.CustomAutoscaler.Hpa.Metrics {
			metric := &v.Spec.CustomAutoscaler.Hpa.Metrics[i]
			metricTarget := GetMetricTarget(&metric.Metric)
			if metricTarget == nil {
				// Early exit because without appending a new error because
				// we already checking metric target elsewhere
				return allErrs
			}
			if metricTarget.Type != metric.ScaleInThreshold.Type {
				err := field.Invalid(pathPrefix.Index(i).Child("scaleInThreshold").Child("type"),
					v.Spec.CustomAutoscaler.Hpa.Metrics[i].ScaleInThreshold.Type,
					fmt.Sprintf("scaleInThreshold type %s must be of the same type as the threshold used for scale out %s",
						metric.ScaleInThreshold.Type, metricTarget.Type),
				)
				allErrs = append(allErrs, err)
			}
		}
	}
	return allErrs
}
