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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/pem"
	"fmt"
	"strconv"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// HTTPSCertRoationReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for https service
type HTTPSCertRoationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeHTTPSCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &HTTPSCertRoationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("HTTPSCertRoationReconciler"),
		Dispatcher: dispatcher,
		Pfacts:     pfacts,
	}
}

// Reconcile will rotate TLS certificate.
func (h *HTTPSCertRoationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}
	if h.Vdb.IsStatusConditionTrue(vapi.HTTPSCertRotationFinished) && h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) {
		return ctrl.Result{}, nil
	}
	currentSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret
	h.Log.Info("starting rotation reconcile, currentSecretName - " + currentSecretName + ", newSecretName - " + newSecretName)
	// this condition excludes bootstrap scenario
	if (newSecretName != "" && currentSecretName == "") || (newSecretName != "" &&
		currentSecretName != "" &&
		newSecretName == currentSecretName) {
		return ctrl.Result{}, nil
	}
	h.Log.Info("rotation is required from " + currentSecretName + " to " + h.Vdb.Spec.NMATLSSecret)
	// rotation is required. Will check start conditions next
	// check if secret is ready for rotation

	currentSecret, newSecret, res, err := h.readSecretsAndConfigMap(ctx, currentSecretName, newSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	h.Log.Info("to start https cert rotation")
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationStarted,
		"Start rotating https cert from %s to %s", currentSecretName, newSecretName)
	cond := vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionTrue, "Started")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		return ctrl.Result{}, err2
	}

	// Now https cert rotation will start
	res, err2 := h.rotateHTTPSTLSCert(ctx, newSecret, currentSecret)
	if verrors.IsReconcileAborted(res, err) {
		h.Log.Info("https cert rotation is aborted.")
		return res, err2
	}
	cond = vapi.MakeCondition(vapi.HTTPSCertRotationFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.HTTPSCertRotationSucceded,
		"Successfully rotated https cert from %s to %s", currentSecretName, newSecretName)
	h.Log.Info("https cert rotation is finished. To rotate nma cert next")
	return ctrl.Result{}, nil
}

// rotateHTTPSTLSCert will rotate https server's tls cert from currentSecret to newSecret
func (h *HTTPSCertRoationReconciler) rotateHTTPSTLSCert(ctx context.Context, newSecret, currentSecret *corev1.Secret) (ctrl.Result, error) {
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run rotate https cert. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	newCert := string(newSecret.Data[corev1.TLSCertKey])
	currentCert := string(currentSecret.Data[corev1.TLSCertKey])
	rotated, err := h.verifyCert(initiatorPod.GetPodIP(), builder.VerticaHTTPPort, newCert, currentCert)
	if err != nil {
		h.Log.Error(err, "https cert rotation aborted. Failed to verify new https cert for "+
			initiatorPod.GetPodIP())
		return ctrl.Result{}, err
	}
	if rotated == 0 {
		h.Log.Info("https cert rotation skipped. new https cert is already in use on " + initiatorPod.GetPodIP())
		return ctrl.Result{}, nil
	}
	if rotated == 2 {
		h.Log.Info("https cert rotation aborted. Neither new or current https cert is in use")
		return ctrl.Result{Requeue: true}, nil
	}
	currentSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	h.Log.Info("ready to rotate certi from " + currentSecretName + " to " + h.Vdb.Spec.NMATLSSecret)
	keyConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, h.Vdb.Namespace)
	certConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, h.Vdb.Namespace)
	caCertConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, h.Vdb.Namespace)
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(string(newSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatehttpscerts.WithPollingCert(newCert),
		rotatehttpscerts.WithPollingCaCert(string(newSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatehttpscerts.WithKey(h.Vdb.Spec.NMATLSSecret, keyConfig),
		rotatehttpscerts.WithCert(h.Vdb.Spec.NMATLSSecret, certConfig),
		rotatehttpscerts.WithCaCert(h.Vdb.Spec.NMATLSSecret, caCertConfig),
		rotatehttpscerts.WithTLSMode("TRY_VERIFY"),
		rotatehttpscerts.WithInitiator(initiatorPod.GetPodIP()),
	}
	h.Log.Info("to call RotateHTTPSCerts for cert " + h.Vdb.Spec.NMATLSSecret + ", tls enabled " +
		strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
	err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate https cer to "+h.Vdb.Spec.NMATLSSecret)
		return ctrl.Result{Requeue: true}, err
	}
	return ctrl.Result{}, err
}

// verifyCert returns 0 when newCert is in use, 1 when currentCert is in use.
// 2 when neither of them is in use
func (h *HTTPSCertRoationReconciler) verifyCert(ip string, port int, newCert, currentCert string) (int, error) {
	conf := &tls.Config{
		InsecureSkipVerify: true, // #nosec G402
	}
	url := fmt.Sprintf("%s:%d", ip, port)
	conn, err := tls.Dial("tcp", url, conf)
	if err != nil {
		h.Log.Error(err, "dial error from verify https cert for "+url)
		return -1, err
	}
	defer conn.Close()
	certs := conn.ConnectionState().PeerCertificates
	for _, cert := range certs {
		var b bytes.Buffer
		err := pem.Encode(&b, &pem.Block{
			Type:  "CERTIFICATE",
			Bytes: cert.Raw,
		})
		if err != nil {
			h.Log.Error(err, "failed to convert cert to PEM for verification")
			return -1, err
		}
		remoteCert := b.String()
		h.Log.Info("raw cert from service - " + url + " - " + remoteCert)
		if newCert == remoteCert {
			return 0, nil
		} else if currentCert == remoteCert {
			return 1, nil
		}
	}
	return 2, nil
}

func (h *HTTPSCertRoationReconciler) readSecretsAndConfigMap(ctx context.Context, currentSecretName,
	newSecretName string) (currentSecret, newSecret *corev1.Secret, res ctrl.Result, err error) {
	nmCurrentSecretName := types.NamespacedName{
		Name:      currentSecretName,
		Namespace: h.Vdb.GetNamespace(),
	}

	nnNewSecretName := types.NamespacedName{
		Name:      newSecretName,
		Namespace: h.Vdb.GetNamespace(),
	}
	evWriter := events.Writer{
		Log:   h.Log,
		EVRec: h.VRec.EVRec,
	}
	secretFetcher := &cloud.SecretFetcher{
		Client:   h.VRec.Client,
		Log:      h.Log,
		EVWriter: evWriter,
		Obj:      h.Vdb,
	}

	currentSecretData, res, err := secretFetcher.FetchAllowRequeue(ctx, nmCurrentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, nil, res, err
	}
	currentSecret = &corev1.Secret{
		Data: currentSecretData,
	}

	newSecretData, res, err := secretFetcher.FetchAllowRequeue(ctx, nnNewSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, nil, res, err
	}
	newSecret = &corev1.Secret{
		Data: newSecretData,
	}
	// check if configmap is ready for rotation
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
	configMapName := types.NamespacedName{
		Name:      name,
		Namespace: h.Vdb.GetNamespace(),
	}
	configMap, res, err := getConfigMap(ctx, h.VRec, h.Vdb, configMapName)
	if verrors.IsReconcileAborted(res, err) {
		return nil, nil, res, err
	}
	if configMap.Data[builder.NMASecretNamespaceEnv] != h.Vdb.GetObjectMeta().GetNamespace() ||
		configMap.Data[builder.NMASecretNameEnv] != newSecretName {
		h.Log.Info(newSecretName + " not found in configmap. cert rotation will not start")
		return nil, nil, ctrl.Result{Requeue: true}, nil
	}
	return currentSecret, newSecret, res, err
}
