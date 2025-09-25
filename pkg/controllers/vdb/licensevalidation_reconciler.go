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
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
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

// LicenseValidationReconciler will check the license
type LicenseValidationReconciler struct {
	vRec       config.ReconcilerInterface
	log        logr.Logger
	vdb        *vapi.VerticaDB
	dispatcher vadmin.Dispatcher
	pFacts     *podfacts.PodFacts
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
	var toValidate = false
	lastSuccessfulValidation := r.vdb.Status.LastLicenseValidation
	if lastSuccessfulValidation.IsZero() {
		toValidate = true
	}
	currentTime := time.Now()
	interval := vmeta.GetLicenseCheckIntervalInMinutes(r.vdb.Annotations)
	if currentTime.After(lastSuccessfulValidation.Time.Add(time.Duration(interval) * time.Minute)) {
		toValidate = true
	}
	if !toValidate {
		return ctrl.Result{}, nil
	}
	r.log.Info("will validate licenses at " + time.Now().String() + ", validated last time at " + lastSuccessfulValidation.String())
	validLicenses, errMsg, err := r.validateLicenses(ctx)
	if err != nil {
		r.log.Info("requeue to wait to validate license")
		return ctrl.Result{Requeue: true}, nil // if no pod, requeue
	}
	if len(validLicenses) == 0 {
		r.vRec.Event(r.vdb, corev1.EventTypeNormal, events.LicenseValidationFail, errMsg)
		return ctrl.Result{}, fmt.Errorf("no valid Vertica license found from the license secret. Details: %s", errMsg)
	}
	r.log.Info("number of valid licenses found from the license secret", len(validLicenses),
		"error messages from validation", errMsg)

	updateTime := metav1.Now()
	r.vdb.Status.LastLicenseValidation = updateTime
	err = r.updateStatus(ctx, updateTime)
	if err != nil {
		r.log.Error(err, "failed to update last license check time")
	}
	r.log.Info("lastLicenseValidation in Status has been set to " + updateTime.String())
	return ctrl.Result{}, nil
}

func (r *LicenseValidationReconciler) validateLicenses(ctx context.Context) (validLicenses []string, errMsg string, err error) {
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
		return validLicenses, errMsg, err
	}
	err = r.pFacts.Collect(ctx, r.vdb)
	if err != nil {
		return validLicenses, errMsg, err
	}
	initiatorPod, ok := r.pFacts.FindRunningPod()
	if !ok {
		err = fmt.Errorf("failed to find an installed pod to verify license")
		return validLicenses, errMsg, err
	}
	initiatorPodIP := initiatorPod.GetPodIP()
	var allErrors error
	for licenseKey, licenseFile := range licenseData {
		opts := []checklicense.Option{
			checklicense.WithInitiators([]string{initiatorPodIP}),
			checklicense.WithLicenseFile(base64.StdEncoding.EncodeToString(licenseFile)),
			checklicense.WithCELienseDisallowed(true),
		}
		r.log.Info("To validate license", "licenseKey", licenseKey, "licenseSecret", r.vdb.Spec.LicenseSecret)
		err2 := r.dispatcher.CheckLicense(ctx, opts...)
		if err2 != nil {
			r.log.Error(err2, "invalid Vertica license", "licenseKey", licenseKey, "licenseSecret", r.vdb.Spec.LicenseSecret)
			allErrors = errors.Join(allErrors, err2)
		} else {
			validLicenses = append(validLicenses, licenseKey)
		}
	}
	if allErrors != nil {
		return validLicenses, allErrors.Error(), nil
	} else {
		return validLicenses, "", nil
	}
}

func (r *LicenseValidationReconciler) updateStatus(ctx context.Context, lastValicationTime metav1.Time) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		vdbChg.Status.LastLicenseValidation = lastValicationTime
		return nil
	}
	return vdbstatus.Update(ctx, r.vRec.GetClient(), r.vdb, updateStatus)
}
