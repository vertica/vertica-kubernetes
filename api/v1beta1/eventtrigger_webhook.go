/*
Copyright [2021-2023] Micro Focus or one of its affiliates.

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
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	allowedNumberReferences = 1
	allowedNumberMatches    = 1
)

// log is for logging in this package.
var eventtriggerlog = logf.Log.WithName("eventtrigger-resource")

func (e *EventTrigger) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(e).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-vertica-com-v1beta1-eventtrigger,mutating=true,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=eventtriggers,verbs=create;update,versions=v1beta1,name=meventtrigger.kb.io,admissionReviewVersions=v1
var _ webhook.Defaulter = &EventTrigger{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (e *EventTrigger) Default() {
	eventtriggerlog.Info("default", "name", e.Name)
}

// +kubebuilder:webhook:path=/validate-vertica-com-v1beta1-verticaautoscaler,mutating=false,failurePolicy=fail,sideEffects=None,groups=vertica.com,resources=verticaautoscalers,verbs=create;update,versions=v1beta1,name=vverticaautoscaler.kb.io,admissionReviewVersions=v1
var _ webhook.Validator = &EventTrigger{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (e *EventTrigger) ValidateCreate() error {
	eventtriggerlog.Info("validate create", "name", e.Name)

	allErrs := e.validateVerticaDBRefSpec()
	if allErrs == nil {
		return nil
	}

	return apierrors.NewInvalid(GkET, e.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (e *EventTrigger) ValidateUpdate(old runtime.Object) error {
	eventtriggerlog.Info("validate update", "name", e.Name)

	allErrs := e.validateVerticaDBRefSpec()
	if allErrs == nil {
		return nil
	}

	return apierrors.NewInvalid(GkET, e.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (e *EventTrigger) ValidateDelete() error {
	eventtriggerlog.Info("validate delete", "name", e.Name)

	return nil
}

func (e *EventTrigger) validateVerticaDBRefSpec() field.ErrorList {
	allErrs := e.validateVerticaDBReferences(field.ErrorList{})
	allErrs = e.validateVerticaDBReferencesSize(allErrs)
	allErrs = e.validateVerticaDBMatchesSize(allErrs)
	if len(allErrs) == 0 {
		return nil
	}

	return allErrs
}

func (e *EventTrigger) validateVerticaDBReferences(allErrs field.ErrorList) field.ErrorList {
	for _, ref := range e.Spec.References {
		if ref.Object.Kind != VerticaDBKind {
			err := field.Invalid(
				field.NewPath("spec").Child("reference").Child("object").Child("kind"),
				ref.Object.Kind,
				fmt.Sprintf("object.kind must be: %s", VerticaDBKind),
			)
			allErrs = append(allErrs, err)
		}

		if ref.Object.APIVersion != GroupVersion.String() {
			err := field.Invalid(
				field.NewPath("spec").Child("reference").Child("object").Child("apiVersion"),
				ref.Object.APIVersion,
				fmt.Sprintf("object.apiVersion must be: %s", GroupVersion.String()),
			)
			allErrs = append(allErrs, err)
		}
	}

	return allErrs
}

func (e *EventTrigger) validateVerticaDBReferencesSize(allErrs field.ErrorList) field.ErrorList {
	ref := e.Spec.References
	if len(ref) > allowedNumberReferences {
		err := field.Invalid(
			field.NewPath("spec").Child("reference"),
			ref,
			fmt.Sprintf("only %d reference object allowed, number received: %d", allowedNumberReferences, len(ref)),
		)
		allErrs = append(allErrs, err)
	}

	return allErrs
}

func (e *EventTrigger) validateVerticaDBMatchesSize(allErrs field.ErrorList) field.ErrorList {
	ref := e.Spec.Matches
	if len(ref) > allowedNumberMatches {
		err := field.Invalid(
			field.NewPath("spec").Child("matches"),
			ref,
			fmt.Sprintf("only %d matches object allowed, number received: %d", allowedNumberMatches, len(ref)),
		)
		allErrs = append(allErrs, err)
	}

	return allErrs
}
