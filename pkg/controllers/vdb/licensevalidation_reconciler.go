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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
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

var lastLicenseValidation metav1.Time

// LicenseValidationReconciler will check the license
type LicenseValidationReconciler struct {
	vRec       config.ReconcilerInterface
	log        logr.Logger
	vdb        *vapi.VerticaDB
	dispatcher vadmin.Dispatcher
	pFacts     *podfacts.PodFacts
}

type LicenseDetail struct {
	Key string `json:"key"`
	vapi.LicenseInfo
}

// MakeLicenseValidationReconciler will build a LicenseReconciler object
func MakeLicenseValidationReconciler(recon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &LicenseValidationReconciler{
		vRec:       recon,
		log:        log.WithName("LicenseValidationReconciler"),
		vdb:        vdb,
		dispatcher: dispatcher,
		pFacts:     pfacts,
	}
}

func (r *LicenseValidationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !r.vdb.UseVClusterOpsDeployment() {
		return ctrl.Result{}, nil
	}
	if r.vdb.Spec.LicenseSecret != "" && (r.vdb.Status.LicenseStatus == nil || r.vdb.Status.LicenseStatus != nil &&
		r.vdb.Spec.LicenseSecret != r.vdb.Status.LicenseStatus.LicenseSecret) {
		res, err := r.validateLicenses(ctx)
		return res, err
	}
	// another scenario
	var toValidate bool
	lastSuccessfulValidation := r.getLastValidattionTimeFromCache()
	if lastSuccessfulValidation.IsZero() {
		toValidate = true
	} else {
		currentTime := time.Now()
		if currentTime.After(lastSuccessfulValidation.Time.Add(time.Duration(24) * time.Hour)) {
			toValidate = true
		}
	}
	if !toValidate {
		return ctrl.Result{}, nil
	}
	r.log.Info("Validate licenses at " + time.Now().String() + ", validated last time at " + lastSuccessfulValidation.String())
	return r.validateLicenses(ctx)
}

func (r *LicenseValidationReconciler) validateLicenses(ctx context.Context) (ctrl.Result, error) {
	if r.vdb.Spec.LicenseSecret == "" {
		r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationFail, "license secret field is empty")
		return ctrl.Result{}, fmt.Errorf("license secret is empty")
	}
	validLicenses, invalidLicenses, err := r.validateLicensesInSecret(ctx)
	if err != nil {
		r.log.Info("requeue to wait to validate license")
		return ctrl.Result{Requeue: true}, nil // if no pod, requeue
	}
	if len(validLicenses) == 0 {
		eventContent := fmt.Sprintf("no valid Vertica license found from secret %s", r.vdb.Spec.LicenseSecret)
		for key, err := range invalidLicenses {
			if strings.Contains(err, "Community Edition") {
				eventContent = fmt.Sprintf("CE license is not allowed and was found in key '%s' of secret '%s'.", key, r.vdb.Spec.LicenseSecret)
			}
		}
		r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationFail, eventContent)
		return ctrl.Result{}, fmt.Errorf("no valid Vertica license found from the license secret")
	}
	if !r.vdb.IsDBInitialized() && meta.GetValidLicenseKey(r.vdb.Annotations) == "" {
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
	newLicenseStatus.Licenses = r.convert(validLicenses)
	err = r.saveLicenseStatusInStatus(ctx, newLicenseStatus)
	if err != nil {
		r.log.Info("failed to save license status into Status")
		return ctrl.Result{}, err
	}
	r.saveLastValidattionTimeInCache(metav1.Now())
	return ctrl.Result{}, nil
}

func (r *LicenseValidationReconciler) validateLicensesInSecret(ctx context.Context) (validLicenses []LicenseDetail,
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
		err2 := r.dispatcher.CheckLicense(ctx, opts...)
		if err2 != nil {
			invalidLicenses[licenseKey] = err2.Error()
			r.log.Error(err2, "invalid Vertica license", "licenseKey", licenseKey, "licenseSecret", r.vdb.Spec.LicenseSecret)
		} else {
			licenseDetail := LicenseDetail{}
			licenseDetail.Key = licenseKey
			licenseDetail.Digest = r.getDigest(string(licenseFile))
			licenseDetail.Valid = true
			validLicenses = append(validLicenses, licenseDetail)
		}
	}
	return validLicenses, invalidLicenses, nil
}

func (r *LicenseValidationReconciler) getDigest(input string) string {
	hasher := sha256.New()
	hasher.Write([]byte(input))
	hashInBytes := hasher.Sum(nil)
	hexHash := hex.EncodeToString(hashInBytes)
	return hexHash
}

func (r *LicenseValidationReconciler) saveLastValidattionTimeInCache(lastValidationTime metav1.Time) {
	lastLicenseValidation = lastValidationTime
}

func (r *LicenseValidationReconciler) getLastValidattionTimeFromCache() metav1.Time {
	return lastLicenseValidation
}

func (r *LicenseValidationReconciler) saveLicenseStatusInStatus(ctx context.Context, licenseStatus *vapi.LicenseStatus) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		vdb.Status.LicenseStatus = licenseStatus
		return nil
	}
	return vdbstatus.Update(ctx, r.vRec.GetClient(), r.vdb, refreshStatusInPlace)
}

func (r *LicenseValidationReconciler) convert(licenseDetails []LicenseDetail) []vapi.LicenseInfo {
	licenseInfoSlice := []vapi.LicenseInfo{}
	for _, licenseDetail := range licenseDetails {
		licenseInfoSlice = append(licenseInfoSlice, licenseDetail.LicenseInfo)
	}
	return licenseInfoSlice
}
