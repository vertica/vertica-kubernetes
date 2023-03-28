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
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
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
	return nil
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (e *EventTrigger) ValidateUpdate(old runtime.Object) error {
	eventtriggerlog.Info("validate update", "name", e.Name)
	return nil
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (e *EventTrigger) ValidateDelete() error {
	eventtriggerlog.Info("validate delete", "name", e.Name)

	return nil
}
