/*
 (c) Copyright [2021-2023] Open Text.
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
	vops "github.com/vertica/vcluster/vclusterops"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var verticarestorepointsquerylog = logf.Log.WithName("verticarestorepointsquery-resource")

func (vrpq *VerticaRestorePointsQuery) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(vrpq).
		Complete()
}

var _ webhook.Defaulter = &VerticaRestorePointsQuery{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) Default() {
	verticarestorepointsquerylog.Info("default", "name", vrpq.Name)
}

var _ webhook.Validator = &VerticaRestorePointsQuery{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) ValidateCreate() error {
	verticarestorepointsquerylog.Info("validate create", "name", vrpq.Name)

	allErrs := vrpq.validateVrpqSpec()
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GkVRPQ, vrpq.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) ValidateUpdate(_ runtime.Object) error {
	verticarestorepointsquerylog.Info("validate update", "name", vrpq.Name)

	allErrs := vrpq.validateVrpqSpec()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GkVRPQ, vrpq.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) ValidateDelete() error {
	verticarestorepointsquerylog.Info("validate delete", "name", vrpq.Name)
	return nil
}

// validateSpec will validate the current VerticaRestorePointsQuery to see if it is valid
func (vrpq *VerticaRestorePointsQuery) validateVrpqSpec() field.ErrorList {
	allErrs := vrpq.validateTimeStamp(field.ErrorList{})
	return allErrs
}

// validateTimeStamp will check if the scalingGranularity field is valid
func (vrpq *VerticaRestorePointsQuery) validateTimeStamp(allErrs field.ErrorList) field.ErrorList {
	if filter := vrpq.Spec.FilterOptions; filter != nil {
		options := vops.ShowRestorePointFilterOptions{}
		options.ArchiveName = &filter.ArchiveName
		options.StartTimestamp = &filter.StartTimestamp
		options.EndTimestamp = &filter.EndTimestamp
		timestampErr := options.ValidateAndStandardizeTimestampsIfAny()
		if timestampErr != nil {
			err := field.Invalid(field.NewPath("spec").Child("filterOptions"),
				filter, timestampErr.Error())
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}
