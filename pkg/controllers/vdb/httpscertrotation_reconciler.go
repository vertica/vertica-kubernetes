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
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// HTTPSCertRotationReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for https service
type HTTPSCertRotationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
}

func MakeHTTPSCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &HTTPSCertRotationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("HTTPSCertRotationReconciler"),
		Dispatcher: dispatcher,
		PFacts:     pfacts,
	}
}

// Reconcile will rotate TLS certificate.
func (h *HTTPSCertRotationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}
	if h.Vdb.IsStatusConditionTrue(vapi.HTTPSCertRotationFinished) && h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) {
		return ctrl.Result{}, nil
	}
	currentSecretName := h.Vdb.GetNMATLSSecretNameInUse()
	newSecretName := h.Vdb.Spec.NMATLSSecret
	h.Log.Info("Starting rotation reconcile", "currentSecretName", currentSecretName, "newSecretName", newSecretName)
	// this condition excludes bootstrap scenario
	if currentSecretName == "" || newSecretName == currentSecretName {
		return ctrl.Result{}, nil
	}
	h.Log.Info("rotation is required from " + currentSecretName + " to " + newSecretName)
	// rotation is required. Will check start conditions next
	// check if secret is ready for rotation

	currentSecretData, newSecretData, res, err := readSecrets(h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx,
		currentSecretName, newSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	h.Log.Info("start https cert rotation")
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationStarted,
		"Start rotating https cert from %s to %s", currentSecretName, newSecretName)
	cond := vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSCertRotationInProgress)
		return ctrl.Result{}, err2
	}

	err = h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Now https cert rotation will start
	res, err2 := h.rotateHTTPSTLSCert(ctx, newSecretData, currentSecretData)
	if verrors.IsReconcileAborted(res, err) {
		h.Log.Info("https cert rotation is aborted.")
		return res, err2
	}
	cond = vapi.MakeCondition(vapi.HTTPSCertRotationFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err2, "failed to set condition "+vapi.HTTPSCertRotationFinished+" to true")
		return ctrl.Result{}, err
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationSucceded,
		"Successfully rotated https cert from %s to %s", currentSecretName, newSecretName)
	h.Log.Info("https cert rotation is finished. To rotate nma cert next")
	return ctrl.Result{}, nil
}

// rotateHTTPSTLSCert will rotate https server's tls cert from currentSecret to newSecret
func (h *HTTPSCertRotationReconciler) rotateHTTPSTLSCert(ctx context.Context, newSecret,
	currentSecret map[string][]byte) (ctrl.Result, error) {
	initiatorPod, ok := h.PFacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run rotate https cert. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	newCert := string(newSecret[corev1.TLSCertKey])
	currentCert := string(currentSecret[corev1.TLSCertKey])
	rotated, err := security.VerifyCert(initiatorPod.GetPodIP(), builder.VerticaHTTPPort, newCert, currentCert, h.Log)
	if err != nil {
		h.Log.Error(err, "https cert rotation aborted. Failed to verify new https cert for "+
			initiatorPod.GetPodIP())
		return ctrl.Result{}, err
	}
	if rotated == 2 {
		h.Log.Info("https cert rotation aborted. Neither new nor current https cert is in use")
		return ctrl.Result{Requeue: true}, nil
	}
	if rotated == 0 {
		h.Log.Info("https cert rotation skipped. new https cert is already in use on " + initiatorPod.GetPodIP())
	} else {
		currentSecretName := h.Vdb.GetNMATLSSecretNameInUse()
		h.Log.Info("ready to rotate certi from " + currentSecretName + " to " + h.Vdb.Spec.NMATLSSecret)

		var keyConfig, certConfig, caCertConfig, secretName string
		switch {
		case secrets.IsAWSSecretsManagerSecret(h.Vdb.Spec.NMATLSSecret):
			keyConfig, certConfig, caCertConfig = h.getAWSCertsConfig()
			secretName = secrets.RemovePathReference(h.Vdb.Spec.NMATLSSecret)
		default:
			keyConfig, certConfig, caCertConfig = h.getK8sCertsConfig()
			secretName = h.Vdb.Spec.NMATLSSecret
		}

		opts := []rotatehttpscerts.Option{
			rotatehttpscerts.WithPollingKey(string(newSecret[corev1.TLSPrivateKeyKey])),
			rotatehttpscerts.WithPollingCert(newCert),
			rotatehttpscerts.WithPollingCaCert(string(newSecret[corev1.ServiceAccountRootCAKey])),
			rotatehttpscerts.WithKey(secretName, keyConfig),
			rotatehttpscerts.WithCert(secretName, certConfig),
			rotatehttpscerts.WithCaCert(secretName, caCertConfig),
			rotatehttpscerts.WithTLSMode("TRY_VERIFY"),
			rotatehttpscerts.WithInitiator(initiatorPod.GetPodIP()),
		}
		h.Log.Info("to call RotateHTTPSCerts for cert " + secretName + ", tls enabled " +
			strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
		err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
		if err != nil {
			h.Log.Error(err, "failed to rotate https cert to "+h.Vdb.Spec.NMATLSSecret)
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, err
}

func (h *HTTPSCertRotationReconciler) getK8sCertsConfig() (string, string, string) {
	keyConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, h.Vdb.Namespace)
	certConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, h.Vdb.Namespace)
	caCertConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, h.Vdb.Namespace)
	return keyConfig, certConfig, caCertConfig
}

func (h *HTTPSCertRotationReconciler) getAWSCertsConfig() (string, string, string) {
	region, _ := secrets.GetAWSRegion(h.Vdb.Spec.NMATLSSecret)

	keyConfig := fmt.Sprintf("{\"json-key\":\"%s\", \"region\":\"%s\"}", corev1.TLSPrivateKeyKey, region)
	certConfig := fmt.Sprintf("{\"json-key\":\"%s\", \"region\":\"%s\"}", corev1.TLSCertKey, region)
	caCertConfig := fmt.Sprintf("{\"json-key\":\"%s\", \"region\":\"%s\"}", paths.HTTPServerCACrtName, region)
	return keyConfig, certConfig, caCertConfig
}
