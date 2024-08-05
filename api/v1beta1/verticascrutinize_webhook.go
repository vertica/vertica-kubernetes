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
	"strconv"
	"strings"
	"time"

	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
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

// validateVscrSpec will validate the current VerticaScrutinize to see if it is valid
func (vscr *VerticaScrutinize) validateVscrSpec() field.ErrorList {
	allErrs := field.ErrorList{}
	allErrs = vscr.ValidateTime(allErrs)
	allErrs = vscr.ValidateLogAgeHours(allErrs)
	allErrs = vscr.ValidateLogAgeTimes(allErrs)
	return allErrs
}

// ValidateLogAgeHours validate the log-age-hours annotation
func (vscr *VerticaScrutinize) ValidateLogAgeHours(allErrs field.ErrorList) field.ErrorList {
	prefix := field.NewPath("metadata").Child("annotations")
	scrutinizeLogAgeHours := vmeta.GetScrutinizeLogAgeHours(vscr.Annotations)
	fmt.Println(scrutinizeLogAgeHours)

	if scrutinizeLogAgeHours != 0 &&
		(vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime] != "" ||
			vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime] != "") {
		err := field.Invalid(prefix.Key(vmeta.ScrutinizeLogAgeHours),
			scrutinizeLogAgeHours,
			"log-age-hours cannot be set alongside log-age-oldest-time and log-age-newest-time")
		allErrs = append(allErrs, err)
	}

	if scrutinizeLogAgeHours < 0 {
		err := field.Invalid(prefix.Key(vmeta.ScrutinizeLogAgeHours),
			scrutinizeLogAgeHours,
			"log-age-hours cannot be negative")
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// log-age-oldest-time should not be ahead of current time.
// log-age-oldest-time should be ahead of logAgeNewestTime.
func (vscr *VerticaScrutinize) ValidateLogAgeTimes(allErrs field.ErrorList) field.ErrorList {
	prefix := field.NewPath("metadata").Child("annotations")
	scrutinizeLogAgeOldesTime := vscr.Annotations[vmeta.ScrutinizeLogAgeOldestTime]
	scrutinizeLogAgeNewestTime := vscr.Annotations[vmeta.ScrutinizeLogAgeNewestTime]

	logAgeNewestTime := time.Now()
	logAgeOldestTime := logAgeNewestTime.Add(-24 * time.Hour) // 24 hours ago

	logAgeArr := [2]string{scrutinizeLogAgeOldesTime, scrutinizeLogAgeNewestTime}
	for i, LogAgeTime := range logAgeArr {
		if LogAgeTime != "" {
			logAgeTime, logAgeError := parseLogAgeTime(LogAgeTime)
			if logAgeError == nil {
				if i == 0 {
					logAgeOldestTime = logAgeTime
				} else {
					logAgeNewestTime = logAgeTime
				}
			} else {
				err := field.Invalid(prefix.Key(vmeta.ScrutinizeLogAgeOldestTime),
					logAgeTime,
					fmt.Sprintf("failed to parse log-age-*-time: %s", logAgeError))
				allErrs = append(allErrs, err)
			}
		}
	}

	if logAgeOldestTime.After(time.Now()) {
		err := field.Invalid(prefix.Key(vmeta.ScrutinizeLogAgeOldestTime),
			scrutinizeLogAgeOldesTime,
			"log-age-oldest-time cannot be set after current time")
		allErrs = append(allErrs, err)
	}

	if logAgeOldestTime.After(logAgeNewestTime) {
		err := field.Invalid(prefix.Key(vmeta.ScrutinizeLogAgeNewestTime),
			scrutinizeLogAgeNewestTime,
			"log-age-oldest-time cannot be set after log-age-newest-time")
		allErrs = append(allErrs, err)
	}

	return allErrs
}

// log-age-oldest-time and log-age-newest-time should be formatted as: YYYY-MM-DD HH [+/-XX],
// where [] is optional and +X represents X hours ahead of UTC.
func (vscr *VerticaScrutinize) ValidateTime(allErrs field.ErrorList) field.ErrorList {
	logAgeArr := [2]string{vmeta.GetScrutinizeLogAgeOldestTime(vscr.Annotations),
		vmeta.GetScrutinizeLogAgeNewestTime(vscr.Annotations)}
	for _, LogAgeTime := range logAgeArr {
		if LogAgeTime != "" {
			// to match pattern: YYYY-MM-DD HH [+/-XX]
			var re = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2} \d{1,2}) ?(?:\+|\-)?(?:\d{2})?$`)
			matches := re.FindAllStringSubmatch(LogAgeTime, -1)

			if matches == nil {
				err := field.Invalid(field.NewPath("Annotations").Child("ScrutinizeLogAgeTime"),
					LogAgeTime,
					fmt.Sprintf("%s should be formatted as: YYYY-MM-DD HH [+/-XX].", LogAgeTime))
				allErrs = append(allErrs, err)
			}
		}
	}

	return allErrs
}

// parseLogAgeTime converts YYYY-MM-DD HH [+/-XX] into time format in UTC
func parseLogAgeTime(logAgeTime string) (time.Time, error) {
	timeArray := strings.Split(logAgeTime, " ")
	logAgeDate := timeArray[0]
	logAgeHour := "00"
	if len(timeArray) > 1 {
		logAgeHour = timeArray[1]
	}
	timeStr := logAgeDate + " " + logAgeHour + ":00:00"

	parseTime, err := time.Parse("2006-01-02 15:04:05", timeStr)
	if err == nil {
		if strings.Contains(logAgeTime, "+") || strings.Contains(logAgeTime, "-") {
			timeZone, zoneErr := strconv.Atoi(timeArray[len(timeArray)-1])
			if zoneErr == nil {
				return parseTime.Add(time.Duration(timeZone) * time.Hour), nil
			}
		}
	} else {
		return parseTime, err
	}

	return parseTime, nil
}
