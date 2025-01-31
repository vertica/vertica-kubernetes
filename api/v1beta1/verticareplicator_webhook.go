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
	"fmt"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var verticareplicatorlog = logf.Log.WithName("verticareplicator-resource")

func (vrep *VerticaReplicator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(vrep).
		Complete()
}

var _ webhook.Defaulter = &VerticaReplicator{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (vrep *VerticaReplicator) Default() {
	verticareplicatorlog.Info("default", "name", vrep.Name)
}

var _ webhook.Validator = &VerticaReplicator{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (vrep *VerticaReplicator) ValidateCreate() (admission.Warnings, error) {
	verticareplicatorlog.Info("validate create", "name", vrep.Name)

	allErrs := vrep.validateVrepSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(GkVR, vrep.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (vrep *VerticaReplicator) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	verticareplicatorlog.Info("validate update", "name", vrep.Name)

	allErrs := vrep.validateVrepSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(GkVR, vrep.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (vrep *VerticaReplicator) ValidateDelete() (admission.Warnings, error) {
	verticareplicatorlog.Info("validate delete", "name", vrep.Name)
	return nil, nil
}

// validateVrepSpec will validate the current VerticaReplicator to see if it is valid
func (vrep *VerticaReplicator) validateVrepSpec() field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = vrep.ValidateReplicationMode(allErrs)
	allErrs = vrep.ValidateAsyncReplicationOptions(allErrs)
	allErrs = vrep.ValidateSyncReplicationOptions(allErrs)
	allErrs = vrep.ValidatePollingFrequency(allErrs)
	return allErrs
}

func (vrep *VerticaReplicator) ValidateReplicationMode(allErrs field.ErrorList) field.ErrorList {
	if vrep.Spec.Mode != "" && vrep.Spec.Mode != ReplicationModeSync && vrep.Spec.Mode != ReplicationModeAsync {
		err := field.Invalid(field.NewPath("spec").Child("mode"),
			vrep.Spec.Mode,
			fmt.Sprintf("Mode must be either '%s' or '%s'", ReplicationModeSync, ReplicationModeAsync))
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// ValidateAsyncReplicationOptions will validate any options for asynchronous replication
func (vrep *VerticaReplicator) ValidateAsyncReplicationOptions(allErrs field.ErrorList) field.ErrorList {
	if vrep.Spec.Mode != ReplicationModeAsync {
		return allErrs
	}

	if vrep.Spec.Source.ObjectName != "" && vrep.Spec.Source.IncludePattern != "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("includePattern"),
			vrep.Spec.Source.IncludePattern,
			"Object name and include pattern cannot be used together")
		allErrs = append(allErrs, err)
	}
	if vrep.Spec.Source.ObjectName != "" && vrep.Spec.Source.ExcludePattern != "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("excludePattern"),
			vrep.Spec.Source.ExcludePattern,
			"Object name and exclude pattern cannot be used together")
		allErrs = append(allErrs, err)
	}
	if vrep.Spec.Source.ExcludePattern != "" && vrep.Spec.Source.IncludePattern == "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("excludePattern"),
			vrep.Spec.Source.ExcludePattern,
			"Exclude pattern cannot be used without include pattern")
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// ValidateSyncReplicationOptions will validate any options for synchronous replication
func (vrep *VerticaReplicator) ValidateSyncReplicationOptions(allErrs field.ErrorList) field.ErrorList {
	if vrep.Spec.Mode != ReplicationModeSync {
		return allErrs
	}

	// Partial replication options should not be used in sync replication mode
	if vrep.Spec.Source.IncludePattern != "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("includePattern"),
			vrep.Spec.Source.IncludePattern,
			fmt.Sprintf("Include pattern cannot be used in replication mode '%s'", ReplicationModeSync))
		allErrs = append(allErrs, err)
	}
	if vrep.Spec.Source.ExcludePattern != "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("excludePattern"),
			vrep.Spec.Source.ExcludePattern,
			fmt.Sprintf("Exclude pattern cannot be used in replication mode '%s'", ReplicationModeSync))
		allErrs = append(allErrs, err)
	}
	if vrep.Spec.Source.ObjectName != "" {
		err := field.Invalid(field.NewPath("spec").Child("source").Child("objectName"),
			vrep.Spec.Source.ObjectName,
			fmt.Sprintf("Object name cannot be used in replication mode '%s'", ReplicationModeSync))
		allErrs = append(allErrs, err)
	}
	if vrep.Spec.Target.Namespace != "" {
		err := field.Invalid(field.NewPath("spec").Child("target").Child("namespace"),
			vrep.Spec.Target.Namespace,
			fmt.Sprintf("Target namespace cannot be used in replication mode '%s'", ReplicationModeSync))
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// ValidatePollingFrequency will validate the poling frequency is larger than 0
func (vrep *VerticaReplicator) ValidatePollingFrequency(allErrs field.ErrorList) field.ErrorList {
	poolingFrequency := vmeta.GetReplicationPollingFrequency(vrep.Annotations)
	if poolingFrequency <= 0 {
		prefix := field.NewPath("metadata").Child("annotations")
		err := field.Invalid(prefix.Key(vmeta.ReplicationPollingFrequencyAnnotation),
			vrep.Annotations[vmeta.ReplicationPollingFrequencyAnnotation],
			"polling frequency cannot be 0 or less than 0")
		allErrs = append(allErrs, err)
	}
	return allErrs
}
