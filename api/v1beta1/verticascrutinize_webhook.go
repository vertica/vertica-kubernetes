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
	"regexp"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// log is for logging in this package.
var verticascrutinizelog = logf.Log.WithName("verticascrutinize-resource")

func (vscr *VerticaScrutinize) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(vscr).
		Complete()
}

var _ webhook.Defaulter = &VerticaScrutinize{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (vscr *VerticaScrutinize) Default() {
	verticascrutinizelog.Info("default", "name", vscr.Name)
}

var _ webhook.Validator = &VerticaScrutinize{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (vscr *VerticaScrutinize) ValidateCreate() error {
	verticascrutinizelog.Info("validate create", "name", vscr.Name)

	allErrs := vscr.validateVscrSpec()
	if len(allErrs) == 0 {
		return nil
	}
	return apierrors.NewInvalid(GkVSCR, vscr.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (vscr *VerticaScrutinize) ValidateUpdate(_ runtime.Object) error {
	verticascrutinizelog.Info("validate update", "name", vscr.Name)

	allErrs := vscr.validateVscrSpec()
	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(GkVSCR, vscr.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (vscr *VerticaScrutinize) ValidateDelete() error {
	verticascrutinizelog.Info("validate delete", "name", vscr.Name)
	return nil
}

// validateSpec will validate the current VerticaScrutinize to see if it is valid
func (vscr *VerticaScrutinize) validateVscrSpec() field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = vscr.ValidateLogAge(allErrs)
	allErrs = vscr.ValidateTime(allErrs)
	return allErrs
}

// validateLogAge will check if an arbitrary time range could be set for the scrutinize logs.
//  1. logAgeHours cannot be set alongside logAgeOldestTime and logAgeNewestTime.
//  2. logAgeOldestTime should not be ahead of current time.
//  3. logAgeOldestTime should be ahead of logAgeNewestTime.
func (vscr *VerticaScrutinize) ValidateLogAge(allErrs field.ErrorList) field.ErrorList {
	if vscr.Spec.LogAgeHours != 0 && (vscr.Spec.LogAgeOldestTime != "" || vscr.Spec.LogAgeNewestTime != "") {
		err := field.Invalid(field.NewPath("Spec").Child("LogAgeHours"),
			vscr.Spec.LogAgeHours,
			"logAgeHours is invalid: logAgeHours cannot be set alongside logAgeOldestTime and logAgeNewestTime")
		allErrs = append(allErrs, err)
	}

	if vscr.Spec.LogAgeHours < 0 {
		err := field.Invalid(field.NewPath("Spec").Child("LogAgeHours"),
			vscr.Spec.LogAgeHours,
			"logAgeHours is invalid: logAgeHours cannot be negative")
		allErrs = append(allErrs, err)
	}

	currentTime := time.Now()
	if vscr.Spec.LogAgeHours == 0 {
		twentyFourHoursAgo := currentTime.Add(-24 * time.Hour)
		logAgeOldestTime := twentyFourHoursAgo

		if vscr.Spec.LogAgeOldestTime != "" {
			logAgeOldestTime = vscr.ParseLogAgeTime(vscr.Spec.LogAgeOldestTime)
			if logAgeOldestTime.After(currentTime) {
				err := field.Invalid(field.NewPath("Spec").Child("LogAgeOldestTime"),
					vscr.Spec.LogAgeOldestTime,
					fmt.Sprintf("logAgeOldestTime %s is invalid: logAgeOldestTime cannot be set after current time",
						vscr.Spec.LogAgeOldestTime))
				allErrs = append(allErrs, err)
			}
		}

		if vscr.Spec.LogAgeNewestTime != "" {
			logAgeNewestTime := vscr.ParseLogAgeTime(vscr.Spec.LogAgeNewestTime)
			if logAgeNewestTime.Before(logAgeOldestTime) {
				err := field.Invalid(field.NewPath("Spec").Child("LogAgeNewestTime"),
					vscr.Spec.LogAgeNewestTime,
					fmt.Sprintf("logAgeNewestTime %s is invalid: logAgeNewestTime cannot be set before logAgeOldestTime",
						vscr.Spec.LogAgeNewestTime))
				allErrs = append(allErrs, err)
			}
		}
	}

	return allErrs
}

// logAgeOldestTime and logAgeNewestTime should be formatted as: YYYY-MM-DD HH [+/-XX],
// where [] is optional and +X represents X hours ahead of UTC.
func (vscr *VerticaScrutinize) ValidateTime(allErrs field.ErrorList) field.ErrorList {
	logAgeArr := [2]string{vscr.Spec.LogAgeOldestTime, vscr.Spec.LogAgeNewestTime}
	for _, LogAgeTime := range logAgeArr {
		if LogAgeTime != "" {
			// to match pattern: YYYY-MM-DD HH [+/-XX]
			var re = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{1,2}) ?(?:\+|\-)?(?:\d{2})?$`)
			matches := re.FindAllStringSubmatch(LogAgeTime, -1)

			if matches == nil {
				err := field.Invalid(field.NewPath("Spec").Child(LogAgeTime),
					LogAgeTime,
					fmt.Sprintf("%s should be formatted as: YYYY-MM-DD HH [+/-XX].", LogAgeTime))
				allErrs = append(allErrs, err)
			}
		}
	}

	return allErrs
}
