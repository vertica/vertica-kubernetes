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

package vdb

import (
	"context"
	"encoding/base64"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cache"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/checklicense"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	lastValidationTimeKey = "lastValidationTime"
)

// LicenseValidationReconciler will check the license
type LicenseValidationReconciler struct {
	vRec         config.ReconcilerInterface
	log          logr.Logger
	vdb          *vapi.VerticaDB
	dispatcher   vadmin.Dispatcher
	pFacts       *podfacts.PodFacts
	cacheManager cache.CacheManager
}

// MakeLicenseValidationReconciler will build a LicenseReconciler object
func MakeLicenseValidationReconciler(recon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts, cacheManager cache.CacheManager) controllers.ReconcileActor {
	return &LicenseValidationReconciler{
		vRec:         recon,
		log:          log.WithName("LicenseValidationReconciler"),
		vdb:          vdb,
		dispatcher:   dispatcher,
		pFacts:       pfacts,
		cacheManager: cacheManager,
	}
}

// Reconcile validates licenses saved in license secret. If no valid license found, an error will be returned.
func (r *LicenseValidationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if r.shouldSkipLicenseValidation() {
		return ctrl.Result{}, nil
	}
	// do validation for new license secret
	if r.licenseValidationRequired() {
		r.log.Info("Validating license", "secret name", r.vdb.Spec.LicenseSecret)
		res, err := r.validateLicenses(ctx)
		return res, err
	}
	lastSuccessfulValidation := r.getLastValidationTimeFromCache()

	// routine license validation on a daily basis
	if !r.shouldDoDailyValidation(lastSuccessfulValidation) {
		return ctrl.Result{}, nil
	}
	r.log.Info("Validating licenses", "last validation time", lastSuccessfulValidation.String(), "current time", time.Now().String())
	return r.validateLicenses(ctx)
}

// shouldSkipLicenseValidation() will return true when license validation should be skipped.
func (r *LicenseValidationReconciler) shouldSkipLicenseValidation() bool {
	return !r.vdb.UseVClusterOpsDeployment() || meta.GetAllowCELicense(r.vdb.Annotations) ||
		r.vdb.IsStatusConditionTrue(vapi.UpgradeInProgress)
}

// licenseValidationRequired() will return true when license validation is required.
func (r *LicenseValidationReconciler) licenseValidationRequired() bool {
	// If no previous status, validation required
	if r.vdb.Status.LicenseStatus == nil {
		return true
	}

	// Validate if the secret name changed
	return r.vdb.Spec.LicenseSecret != r.vdb.Status.LicenseStatus.LicenseSecret
}

// shouldDoDailyValidation() will return true when it is time to do a daily license validation.
// The last successful license validation time is saved in cache. This function compares the current time with
// the cached time. If the interval is longer than a day, it will return true. If no cached time found, it
// also returns true.
func (r *LicenseValidationReconciler) shouldDoDailyValidation(lastSuccessfulValidation metav1.Time) bool {
	var toValidate bool
	if lastSuccessfulValidation.IsZero() {
		toValidate = true
	} else {
		currentTime := time.Now()
		if currentTime.After(lastSuccessfulValidation.Time.Add(time.Duration(24) * time.Hour)) {
			toValidate = true
		}
	}
	return toValidate
}

// validateLicenses() will call validateLicensesInSecret() to validate all licenses found in the license secret
// and save their validness into status. If no valid license found from the secret, it will return an error.
// That will fail the reconciliation loop. The validation time will be saved into the cache.
func (r *LicenseValidationReconciler) validateLicenses(ctx context.Context) (ctrl.Result, error) {
	if r.vdb.Spec.LicenseSecret == "" {
		r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationFail, "license secret field is empty")
		return ctrl.Result{}, fmt.Errorf("license secret is empty")
	}
	validLicenses, invalidLicenses, err := r.validateLicensesInSecret(ctx)
	if err != nil {
		r.log.Info("failed to validate license")
		return ctrl.Result{}, err // if no pod, return an error
	}
	var eventContent string
	var hasCELicense bool
	for key, err := range invalidLicenses {
		if strings.Contains(err, "Community Edition") {
			eventContent = fmt.Sprintf("CE license is not allowed and was found in key '%s' of secret '%s'.", key, r.vdb.Spec.LicenseSecret)
			hasCELicense = true
		}
	}

	if hasCELicense || len(validLicenses) == 0 {
		if !hasCELicense {
			eventContent = fmt.Sprintf("no valid Vertica license found from secret %s", r.vdb.Spec.LicenseSecret)
		}
		r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationFail, eventContent)
		return ctrl.Result{}, fmt.Errorf("%s", eventContent)
	}
	// When db is not created yet, save a valid license key into annotation for db creation
	if !r.vdb.IsDBInitialized() {
		r.vdb.Annotations[meta.ValidLicenseKeyAnnotation] = validLicenses[0].Key
	}
	err = r.vRec.GetClient().Update(ctx, r.vdb)
	if err != nil {
		r.log.Info("failed to update vdb after license validation")
		return ctrl.Result{}, err
	}
	r.log.Info("Successfully validated license secret", "secret name", r.vdb.Spec.LicenseSecret, "number of valid licenses",
		len(validLicenses), "keys of invalid licenses", strings.Join(slices.Collect(maps.Keys(invalidLicenses)), ","),
		"error messages from validation", strings.Join(slices.Collect(maps.Values(invalidLicenses)), ","))
	r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationSucceeded,
		fmt.Sprintf("%d valid Vertica license found from secret '%s'",
			len(validLicenses), r.vdb.Spec.LicenseSecret))
	newLicenseStatus := &vapi.LicenseStatus{}
	newLicenseStatus.LicenseSecret = r.vdb.Spec.LicenseSecret
	newLicenseStatus.Licenses = validLicenses
	err = r.saveLicenseStatusInStatus(ctx, newLicenseStatus)
	if err != nil {
		r.log.Info("failed to save license status into Status")
		return ctrl.Result{}, err
	}
	r.saveLastValidationTimeInCache(metav1.Now())
	return ctrl.Result{}, nil
}

// validateLicensesInSecret() load licenses from the secret and calls vcluster API to validate all licenses
// It returns valid licenses, invalid licenses and the error messages.
func (r *LicenseValidationReconciler) validateLicensesInSecret(ctx context.Context) (validLicenses []vapi.LicenseInfo,
	invalidLicenses map[string]string, err error) {
	namespacedLicenseSecretName := types.NamespacedName{
		Name:      r.vdb.Spec.LicenseSecret,
		Namespace: r.vdb.Namespace,
	}
	secretFetcher := &cloud.SecretFetcher{
		Client:   r.vRec.GetClient(),
		Log:      r.log,
		Obj:      r.vdb,
		EVWriter: r.vRec.GetEventRecorder(),
	}
	licenseData, _, err := secretFetcher.FetchAllowRequeue(ctx, namespacedLicenseSecretName)
	if err != nil {
		return validLicenses, invalidLicenses, err
	}
	err = r.pFacts.Collect(ctx, r.vdb)
	if err != nil {
		return validLicenses, invalidLicenses, err
	}
	initiatorPod, ok := r.pFacts.FindRunningPod()
	if !ok {
		err = fmt.Errorf("failed to find an installed pod to verify license")
		return validLicenses, invalidLicenses, err
	}
	initiatorPodIP := initiatorPod.GetPodIP()
	invalidLicenses = make(map[string]string, len(licenseData))
	for licenseKey, licenseFile := range licenseData {
		opts := []checklicense.Option{
			checklicense.WithInitiators([]string{initiatorPodIP}),
			checklicense.WithLicenseFile(base64.StdEncoding.EncodeToString(licenseFile)),
			checklicense.WithCELienseDisallowed(true),
		}
		r.log.Info("To validate license", "licenseKey", licenseKey, "licenseSecret", r.vdb.Spec.LicenseSecret)
		errCheckLicense := r.dispatcher.CheckLicense(ctx, opts...)
		if errCheckLicense != nil {
			invalidLicenses[licenseKey] = errCheckLicense.Error()
			r.log.Error(errCheckLicense, "invalid Vertica license", "licenseKey", licenseKey, "licenseSecret", r.vdb.Spec.LicenseSecret)
		} else {
			licenseInfo := vapi.LicenseInfo{}
			licenseInfo.Key = licenseKey
			licenseInfo.Valid = true
			validLicenses = append(validLicenses, licenseInfo)
		}
	}
	return validLicenses, invalidLicenses, nil
}

// saveLastValidattionTimeInCache() saves the validation time into the cache
func (r *LicenseValidationReconciler) saveLastValidationTimeInCache(lastValidationTime metav1.Time) {
	timestampCache := r.cacheManager.GetTimestampCacheForVdb(r.vdb.Namespace, r.vdb.Name)
	timestampCache.Set(lastValidationTimeKey, lastValidationTime)
}

// getLastValidattionTimeFromCache() loads last successful validation time from the cache
func (r *LicenseValidationReconciler) getLastValidationTimeFromCache() metav1.Time {
	timestampCache := r.cacheManager.GetTimestampCacheForVdb(r.vdb.Namespace, r.vdb.Name)
	timestamp, ok := timestampCache.Get(lastValidationTimeKey)
	if ok {
		return timestamp
	}
	var lastLicenseValidation metav1.Time
	return lastLicenseValidation
}

// saveLicenseStatusInStatus saves licenses' validness status into Status
func (r *LicenseValidationReconciler) saveLicenseStatusInStatus(ctx context.Context, licenseStatus *vapi.LicenseStatus) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		vdb.Status.LicenseStatus = licenseStatus
		return nil
	}
	return vdbstatus.Update(ctx, r.vRec.GetClient(), r.vdb, refreshStatusInPlace)
}
