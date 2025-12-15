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

	vops "github.com/vertica/vcluster/vclusterops"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
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
func (vrpq *VerticaRestorePointsQuery) ValidateCreate() (admission.Warnings, error) {
	verticarestorepointsquerylog.Info("validate create", "name", vrpq.Name)

	allErrs := vrpq.validateVrpqSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}
	return nil, apierrors.NewInvalid(GkVRPQ, vrpq.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) ValidateUpdate(_ runtime.Object) (admission.Warnings, error) {
	verticarestorepointsquerylog.Info("validate update", "name", vrpq.Name)

	allErrs := vrpq.validateVrpqSpec()
	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(GkVRPQ, vrpq.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (vrpq *VerticaRestorePointsQuery) ValidateDelete() (admission.Warnings, error) {
	verticarestorepointsquerylog.Info("validate delete", "name", vrpq.Name)
	return nil, nil
}

// validateSpec will validate the current VerticaRestorePointsQuery to see if it is valid
func (vrpq *VerticaRestorePointsQuery) validateVrpqSpec() field.ErrorList {
	var allErrs field.ErrorList
	switch vrpq.Spec.QueryType {
	case ShowRestorePoints:
		allErrs = vrpq.hasValidShowRestorePointsConfig(allErrs)
	case SaveRestorePoint:
		allErrs = vrpq.hasValidSaveRestorePointConfig(allErrs)
	default:
		err := field.Invalid(field.NewPath("spec").Child("queryType"),
			vrpq.Spec.QueryType,
			"queryType must be one of ShowRestorePoints or SaveRestorePoint")
		allErrs = append(allErrs, err)
	}
	return allErrs
}

// validateTimeStamp will check if all non-empty timestamps specified have valid date time or date only format
// convert date only format to date time format when applicable, and make sure end timestamp
// is no earlier than start timestamp
func (vrpq *VerticaRestorePointsQuery) validateTimeStamp(allErrs field.ErrorList) field.ErrorList {
	if filter := vrpq.Spec.FilterOptions; filter != nil {
		options := vops.ShowRestorePointFilterOptions{}
		options.ArchiveName = filter.ArchiveName
		options.StartTimestamp = filter.StartTimestamp
		options.EndTimestamp = filter.EndTimestamp
		timestampErr := options.ValidateAndStandardizeTimestampsIfAny()
		if timestampErr != nil {
			err := field.Invalid(field.NewPath("spec").Child("filterOptions"),
				filter, timestampErr.Error())
			allErrs = append(allErrs, err)
		}
	}
	return allErrs
}

func (vrpq *VerticaRestorePointsQuery) hasValidShowRestorePointsConfig(allErrs field.ErrorList) field.ErrorList {
	if vrpq.Spec.FilterOptions == nil {
		return allErrs
	}
	if vrpq.Spec.FilterOptions.ArchiveName != "" {
		invalidChars := findInvalidChars(vrpq.Spec.FilterOptions.ArchiveName, true)
		if invalidChars != "" {
			err := field.Invalid(field.NewPath("spec").Child("filterOptions").Child("archiveName"),
				vrpq.Spec.FilterOptions.ArchiveName,
				fmt.Sprintf(`archiveName cannot have the characters %q`, invalidChars))
			allErrs = append(allErrs, err)
		}
	}

	return vrpq.validateTimeStamp(allErrs)
}

func (vrpq *VerticaRestorePointsQuery) hasValidSaveRestorePointConfig(allErrs field.ErrorList) field.ErrorList {
	if vrpq.Spec.SaveOptions == nil {
		err := field.Invalid(field.NewPath("spec").Child("saveOptions"),
			vrpq.Spec.SaveOptions,
			"saveOptions must be specified when queryType is SaveRestorePoint.")
		allErrs = append(allErrs, err)
		return allErrs
	}
	if vrpq.Spec.SaveOptions.Archive == "" {
		err := field.Invalid(field.NewPath("spec").Child("saveOptions").Child("archive"),
			vrpq.Spec.SaveOptions.Archive,
			"archive must be specified when queryType is SaveRestorePoint.")
		allErrs = append(allErrs, err)
	} else {
		invalidChars := findInvalidChars(vrpq.Spec.SaveOptions.Archive, true)
		if invalidChars != "" {
			err := field.Invalid(field.NewPath("spec").Child("saveOptions").Child("archive"),
				vrpq.Spec.SaveOptions.Archive,
				fmt.Sprintf(`archive cannot have the characters %q`, invalidChars))
			allErrs = append(allErrs, err)
		}
	}
	if vrpq.Spec.SaveOptions.NumRestorePoints < 0 {
		err := field.Invalid(field.NewPath("spec").Child("saveOptions").Child("numRestorePoints"),
			vrpq.Spec.SaveOptions.NumRestorePoints,
			"numRestorePoints must be set to 0 or greater")
		allErrs = append(allErrs, err)
	}
	return allErrs
}
