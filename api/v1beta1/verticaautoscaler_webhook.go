/*
Copyright [2021-2022] Micro Focus or one of its affiliates.

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
package v1beta1

import (
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var verticaautoscalerlog = logf.Log.WithName("verticaautoscaler-resource")

func (v *VerticaAutoscaler) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(v).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-vertica-com-v1beta1-verticaautoscaler,mutating=true,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticaautoscalers,verbs=create;update,versions=v1beta1,name=mverticaautoscaler.kb.io,admissionReviewVersions=v1
var _ webhook.Defaulter = &VerticaDB{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (v *VerticaAutoscaler) Default() {
	verticaautoscalerlog.Info("default", "name", v.Name)

	if v.Spec.Template.ServiceName == "" {
		v.Spec.Template.ServiceName = v.Spec.ServiceName
	}
}

//+kubebuilder:webhook:path=/validate-vertica-com-v1beta1-verticaautoscaler,mutating=false,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticaautoscalers,verbs=create;update,versions=v1beta1,name=vverticaautoscaler.kb.io,admissionReviewVersions=v1
var _ webhook.Validator = &VerticaAutoscaler{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateCreate() error {
	verticaautoscalerlog.Info("validate create", "name", v.Name)

	allErrs := v.validateSpec(true)
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaAutoscalerKind}, v.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateUpdate(old runtime.Object) error {
	verticaautoscalerlog.Info("validate update", "name", v.Name)

	allErrs := v.validateSpec(false)
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(schema.GroupKind{Group: Group, Kind: VerticaAutoscalerKind}, v.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (v *VerticaAutoscaler) ValidateDelete() error {
	verticaautoscalerlog.Info("validate delete", "name", v.Name)

	return nil
}

// validateSpec will validate the current VerticaAutoscaler to see if it is valid
func (v *VerticaAutoscaler) validateSpec(isCreate bool) field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = v.validateScalingGranularity(allErrs)
	allErrs = v.validateSubclusterTemplate(allErrs, isCreate)
	return allErrs
}

// validateScalingGranularity will check if the scalingGranularity field is valid
func (v *VerticaAutoscaler) validateScalingGranularity(allErrs field.ErrorList) field.ErrorList {
	switch v.Spec.ScalingGranularity {
	case PodScalingGranularity, SubclusterScalingGranularity:
		return allErrs
	default:
		err := field.Invalid(field.NewPath("spec").Child("scalingGranularity"),
			v.Spec.ScalingGranularity,
			fmt.Sprintf("scalingGranularity must be set to either %s or %s",
				SubclusterScalingGranularity,
				PodScalingGranularity))
		return append(allErrs, err)
	}
}

// validateSubclusterTemplate will validate the subcluster template
func (v *VerticaAutoscaler) validateSubclusterTemplate(allErrs field.ErrorList, isCreate bool) field.ErrorList {
	pathPrefix := field.NewPath("spec").Child("template")
	// We have a defaulter that sets the service name in template to match
	// spec.serviceName.  So we only need to check for differences if this is an
	// update or a create but we set something.
	if (!isCreate || v.Spec.Template.ServiceName != "") &&
		v.Spec.Template.ServiceName != v.Spec.ServiceName {
		err := field.Invalid(pathPrefix.Child("serviceName"),
			v.Spec.Template.ServiceName,
			"The serviceName in the subcluster template must match spec.serviceName")
		allErrs = append(allErrs, err)
	}

	return allErrs
}
