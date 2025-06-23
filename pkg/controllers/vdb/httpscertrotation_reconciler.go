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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
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

const (
	noTLSChange = iota
	tlsModeChangeOnly
	httpsCertChangeOnly
	tlsModeAndCertChange
)

type HTTPSCertRotationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
}

type httpsTLSUpdateData struct {
	key         string
	cert        string
	caCert      string
	tlsMode     string
	initiatorIP string
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

	changeTLS := h.updateTLSConfig()
	// no-op if neither https secret nor tls mode
	// changed
	if changeTLS == noTLSChange {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start https cert rotation")
	cond := vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSCertRotationInProgress)
		return ctrl.Result{}, err2
	}

	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	tlsData, res, err := h.buildHTTPSTLSUpdateData(ctx, changeTLS)
	if tlsData == nil || verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Now https cert rotation will start
	err2 := h.rotateHTTPSTLSCert(ctx, tlsData, changeTLS)
	if err2 != nil {
		return ctrl.Result{}, err2
	}
	currentTLSMode := h.Vdb.GetHTTPSTLSModeInUse()
	if currentTLSMode != h.Vdb.Spec.HTTPSNMATLS.Mode {
		httpsTLSConfig := vapi.MakeHTTPSNMATLSConfigFromSpec(h.Vdb.Spec.HTTPSNMATLS)
		err = vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSConfig{httpsTLSConfig})
		if err != nil {
			h.Log.Error(err, "failed to update tls mode after https cert rotation")
			return ctrl.Result{}, err
		}
	}
	h.Log.Info(fmt.Sprintf("https tls mode is changed to %s after https cert rotation", h.Vdb.Spec.HTTPSNMATLS.Mode))
	cond = vapi.MakeCondition(vapi.HTTPSCertRotationFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.HTTPSCertRotationFinished+" to true")
		return ctrl.Result{}, err
	}
	// Clear TLSCertRotationInProgress condition if only tls mode changed.
	// This way, we will skip nma cert rotation
	if changeTLS == tlsModeChangeOnly {
		cond = vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionFalse, "Completed")
		if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// rotateHTTPSTLSCert will rotate https server's tls cert from currentSecret to newSecret
func (h *HTTPSCertRotationReconciler) rotateHTTPSTLSCert(ctx context.Context, tlsData *httpsTLSUpdateData,
	updateType int) error {
	if updateType == tlsModeAndCertChange || updateType == httpsCertChangeOnly {
		h.Log.Info("ready to rotate https cert from " + h.Vdb.GetHTTPSTLSSecretNameInUse() + " to " + h.Vdb.Spec.HTTPSNMATLS.Secret)
	}
	if updateType == tlsModeAndCertChange || updateType == tlsModeChangeOnly {
		h.Log.Info(fmt.Sprintf("ready to change HTTPS TLS mode from %s to %s", h.Vdb.GetHTTPSTLSModeInUse(), tlsData.tlsMode))
	}

	var keyConfig, certConfig, caCertConfig, secretName string
	switch {
	case secrets.IsAWSSecretsManagerSecret(h.Vdb.Spec.HTTPSNMATLS.Secret):
		keyConfig, certConfig, caCertConfig = GetAWSCertsConfig(h.Vdb)
		secretName = secrets.RemovePathReference(h.Vdb.Spec.HTTPSNMATLS.Secret)
	default:
		keyConfig, certConfig, caCertConfig = GetK8sCertsConfig(h.Vdb)
		secretName = h.Vdb.Spec.HTTPSNMATLS.Secret
	}
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(tlsData.key),
		rotatehttpscerts.WithPollingCert(tlsData.cert),
		rotatehttpscerts.WithPollingCaCert(tlsData.caCert),
		rotatehttpscerts.WithKey(secretName, keyConfig),
		rotatehttpscerts.WithCert(secretName, certConfig),
		rotatehttpscerts.WithCaCert(secretName, caCertConfig),
		rotatehttpscerts.WithTLSMode(tlsData.tlsMode),
		rotatehttpscerts.WithInitiator(tlsData.initiatorIP),
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationStarted,
		"Starting https cert rotation with secret name %s and mode %s",
		h.Vdb.Spec.HTTPSNMATLS.Secret, tlsData.tlsMode)
	err := h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.VRec.Eventf(h.Vdb, corev1.EventTypeWarning, events.HTTPSCertRotationFailed,
			"Failed to rotate https cert with secret name %s and mode %s", h.Vdb.Spec.HTTPSNMATLS.Secret, tlsData.tlsMode)
		return err
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationSucceeded,
		"Successfully rotated https cert with secret name %s and mode %s", h.Vdb.Spec.HTTPSNMATLS.Secret, tlsData.tlsMode)

	return err
}

// buildHTTPSTLSUpdateData constructs the data needed for tls update
func (h *HTTPSCertRotationReconciler) buildHTTPSTLSUpdateData(ctx context.Context,
	updateType int) (*httpsTLSUpdateData, ctrl.Result, error) {
	var currentSecretData map[string][]byte
	var newSecretData map[string][]byte
	var res ctrl.Result
	var err error
	tlsData := &httpsTLSUpdateData{}

	tlsData.tlsMode = h.Vdb.Spec.HTTPSNMATLS.Mode
	currentSecretName := h.Vdb.GetHTTPSTLSSecretNameInUse()
	newSecretName := h.Vdb.Spec.HTTPSNMATLS.Secret
	currentSecretData, res, err = readSecret(h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx, currentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, res, err
	}
	initiatorPod, ok := h.PFacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run vsql to update tls mode. Requeue reconciliation.")
		return nil, ctrl.Result{Requeue: true}, nil
	}
	tlsData.initiatorIP = initiatorPod.GetPodIP()
	if updateType != tlsModeChangeOnly {
		newSecretData, res, err = readSecret(h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx, newSecretName)
		if verrors.IsReconcileAborted(res, err) {
			return nil, res, err
		}
		tlsData.key = string(newSecretData[corev1.TLSPrivateKeyKey])
		tlsData.cert = string(newSecretData[corev1.TLSCertKey])
		tlsData.caCert = string(newSecretData[corev1.ServiceAccountRootCAKey])
		return tlsData, res, err
	}

	tlsData.key = string(currentSecretData[corev1.TLSPrivateKeyKey])
	tlsData.cert = string(currentSecretData[corev1.TLSCertKey])
	tlsData.caCert = string(currentSecretData[corev1.ServiceAccountRootCAKey])

	return tlsData, res, err
}

func GetK8sCertsConfig(vdb *vapi.VerticaDB) (keyConfig, certConfig, caCertConfig string) {
	keyConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, vdb.Namespace)
	certConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, vdb.Namespace)
	caCertConfig = fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, vdb.Namespace)
	return
}

func GetAWSCertsConfig(vdb *vapi.VerticaDB) (keyConfig, certConfig, caCertConfig string) {
	region, _ := secrets.GetAWSRegion(vdb.Spec.HTTPSNMATLS.Secret)

	keyConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSPrivateKeyKey, region)
	certConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", corev1.TLSCertKey, region)
	caCertConfig = fmt.Sprintf("{\"json-key\":%q, \"region\":%q}", paths.HTTPServerCACrtName, region)
	return
}

func (h *HTTPSCertRotationReconciler) updateTLSConfig() int {
	currentSecretName := h.Vdb.GetHTTPSTLSSecretNameInUse()
	newSecretName := h.Vdb.Spec.HTTPSNMATLS.Secret
	h.Log.Info("Starting rotation reconcile", "currentSecretName", currentSecretName, "newSecretName", newSecretName)
	// this condition excludes bootstrap scenario
	certChanged := currentSecretName != "" && newSecretName != currentSecretName

	if h.Vdb.Spec.HTTPSNMATLS.Mode != h.Vdb.GetHTTPSTLSModeInUse() {
		if certChanged {
			return tlsModeAndCertChange
		}
		return tlsModeChangeOnly
	} else if certChanged {
		return httpsCertChangeOnly
	}

	return noTLSChange
}
