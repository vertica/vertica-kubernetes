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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSServerCertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the https server.
type TLSCertRoationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSCertRoationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSCertRoationReconciler"),
		Dispatcher: dispatcher,
		Pfacts:     pfacts,
	}
}

// Reconcile will rotate TLS certificate.
func (h *TLSCertRoationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if vmeta.UseNMACertsMount(h.Vdb.Annotations) || !vmeta.EnableTLSCertsRotation(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}
	curretSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret
	h.Log.Info("starting rotation reconcile, currentSecretName - " + curretSecretName + ", newSecretName - " + newSecretName)
	// this condition excludes bootstrap scenario
	if (newSecretName != "" && curretSecretName == "") || (newSecretName != "" &&
		curretSecretName != "" &&
		newSecretName == curretSecretName) {
		return ctrl.Result{}, nil
	}
	h.Log.Info("rotation is required from " + curretSecretName + " to " + h.Vdb.Spec.NMATLSSecret)
	// rotation is required. Will check start conditions next
	// check if secret is ready for rotation
	currentSecret := corev1.Secret{}
	found, err := vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &curretSecretName, &currentSecret)
	if !found || err != nil {
		h.Log.Info("current secret is not ready yet for rotation. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	newSecret := corev1.Secret{}
	found, err = vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &newSecretName, &newSecret)
	if !found || err != nil {
		h.Log.Info("new secret is not ready for rotation. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	// check if configmap is ready for rotation
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
	configMapName := types.NamespacedName{
		Name:      name,
		Namespace: h.Vdb.GetNamespace(),
	}
	configMap := &corev1.ConfigMap{}
	err = h.VRec.Client.Get(ctx, configMapName, configMap)
	if err != nil {
		h.Log.Info("failed to retrieve configmap for rotation. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	if configMap.Data[builder.NMASecretNamespaceEnv] != h.Vdb.GetObjectMeta().GetNamespace() ||
		configMap.Data[builder.NMASecretNameEnv] != newSecretName {
		h.Log.Info(newSecretName + " not found in configmap. cert rotation will not start")
		return ctrl.Result{Requeue: true}, nil
	}
	h.Log.Info("to start https cert rotation")
	// Now https cert rotation will start
	res, err := h.rotateHTTPSTLSCert(ctx, &newSecret, &currentSecret)
	if verrors.IsReconcileAborted(res, err) {
		h.Log.Info("https cert rotation is aborted.")
		return res, err
	}
	h.Log.Info("https cert rotation is finished. To rotate nma cert next")
	return h.rotateNmaTLSCert(ctx, &newSecret, &currentSecret)
}

// rotateHTTPSTLSCert will rotate node management agent's tls cert from currentSecret to newSecret
func (h *TLSCertRoationReconciler) rotateNmaTLSCert(ctx context.Context, newSecret, currentSecret *corev1.Secret) (ctrl.Result, error) {
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run rotate nma cert. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	currentSecretName := meta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret

	newCert := string(newSecret.Data[corev1.TLSCertKey])
	currentCert := string(currentSecret.Data[corev1.TLSCertKey])
	rotated, err := h.verifyCert(initiatorPod.GetPodIP(), builder.NMAPort, newCert, currentCert)
	if err != nil {
		h.Log.Error(err, "nma cert rotation aborted. Failed to verify new https cert for "+
			initiatorPod.GetPodIP())
		return ctrl.Result{}, err
	}
	if rotated == 2 {
		h.Log.Info("nma cert rotation skipped. Neither new or existing nma cert is in use on " +
			initiatorPod.GetPodIP())
		return ctrl.Result{Requeue: true}, nil
	}
	if rotated == 0 {
		h.Log.Info("nma cert rotation skipped. new nma cert for " +
			" is already in use on " + initiatorPod.GetPodIP())
		return ctrl.Result{}, nil
	}

	h.Log.Info("to rotate nma certi from " + currentSecretName + " to " + newSecretName)
	h.Pfacts.Collect(ctx, h.Vdb)
	hosts := []string{}
	for _, detail := range h.Pfacts.Detail {
		hosts = append(hosts, detail.GetPodIP())
	}
	opts := []rotatenmacerts.Option{
		rotatenmacerts.WithKey(string(newSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatenmacerts.WithCert(string(newSecret.Data[corev1.TLSCertKey])),
		rotatenmacerts.WithCaCert(string(newSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatenmacerts.WithHosts(hosts),
	}

	vdbContext := vadmin.GetContextForVdb(h.Vdb.Namespace, h.Vdb.Name)
	h.Log.Info("to call RotateNMACerts, use tls " + strconv.FormatBool(vdbContext.GetBoolValue(vadmin.UseTLSCert)))
	err = h.Dispatcher.RotateNMACerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate nma cer to "+newSecretName)
		return ctrl.Result{}, err
	}
	result, err2 := h.checkCertAfterRoation("nma", initiatorPod.GetPodIP(), builder.NMAPort, newSecretName, newCert, currentCert)
	if !result.Requeue && err2 == nil { // if rotation succeeds update annotations
		previousTLSSecretName := meta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
		err = vk8s.UpdateAnnotation(vmeta.NMATLSSecretPreviouslyUsedAnnotation, previousTLSSecretName, h.Vdb, ctx, h.VRec.Client, h.Log)
		if err != nil {
			h.Log.Error(err, "failed to save previously used tls cert secret name in annotation after cert rotation")
			return ctrl.Result{}, err
		}
		h.Log.Info("saved previously used tls cert secret name " + previousTLSSecretName + " in annotation")
		err = vk8s.UpdateAnnotation(vmeta.NMATLSSecretInUseAnnotation, newSecretName, h.Vdb, ctx, h.VRec.Client, h.Log)
		if err != nil {
			h.Log.Error(err, "failed to save new tls cert secret name in annotation after cert rotation")
			return ctrl.Result{}, err
		}
		h.Log.Info("saved new tls cert secret name " + newSecretName + " in annotation")
		// last thing is to update vdb condition
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NmaTLSCertRotated,
			"Successfully rotate tls cert from %s to %s", currentSecretName, newSecretName)
	}
	return result, err2
}

// rotateHTTPSTLSCert will rotate https server's tls cert from currentSecret to newSecret
func (h *TLSCertRoationReconciler) rotateHTTPSTLSCert(ctx context.Context, newSecret, currentSecret *corev1.Secret) (ctrl.Result, error) {
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
	currentSecretName := meta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	h.Log.Info("ready to rotate certi from " + currentSecretName + " to " + h.Vdb.Spec.NMATLSSecret)
	keyConfig := fmt.Sprintf("{\"data-key\":\"%s\", \"namespace\":\"%s\"}", corev1.TLSPrivateKeyKey, h.Vdb.Namespace)
	certConfig := fmt.Sprintf("{\"data-key\":\"%s\", \"namespace\":\"%s\"}", corev1.TLSCertKey, h.Vdb.Namespace)
	caCertConfig := fmt.Sprintf("{\"data-key\":\"%s\", \"namespace\":\"%s\"}", paths.HTTPServerCACrtName, h.Vdb.Namespace)
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
	vdbContext := vadmin.GetContextForVdb(h.Vdb.Namespace, h.Vdb.Name)
	h.Log.Info("to call RotateHTTPSCerts for cert " + h.Vdb.Spec.NMATLSSecret + ", use tls " +
		strconv.FormatBool(vdbContext.GetBoolValue(vadmin.UseTLSCert)))
	err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate https cer to "+h.Vdb.Spec.NMATLSSecret)
		return ctrl.Result{Requeue: true}, err
	}
	return h.checkCertAfterRoation("https", initiatorPod.GetPodIP(), builder.VerticaHTTPPort, h.Vdb.Spec.NMATLSSecret, newCert, currentCert)
}

// checkCertAfterRoation will return different result and error based on result from calling verifyCert
func (h *TLSCertRoationReconciler) checkCertAfterRoation(moduleName, ip string, port int, newCertName, newCert, currentCert string) (ctrl.Result, error) {
	rotated, err := h.verifyCert(ip, port, newCert, currentCert)
	if err != nil {
		h.Log.Error(err, moduleName+" cert rotation aborted. Failed to verify new cert "+newCertName+" on "+
			ip)
		return ctrl.Result{}, err
	}
	if rotated == 1 {
		h.Log.Info(moduleName + " cert rotation is NOT successful. Current cert " +
			" is still in use on " + ip)
		return ctrl.Result{Requeue: true}, nil
	}
	if rotated == 2 {
		h.Log.Info(moduleName + " cert rotation is NOT successful. Neither of new or current certs " +
			" is in use on " + ip)
		return ctrl.Result{Requeue: true}, nil
	}
	h.Log.Info(moduleName + " cert rotation is successful. New cert " + newCertName +
		" is already in use on " + ip)
	return ctrl.Result{}, nil
}

// verifyCert returns 0 when newCert is in use, 1 when currentCert is in use.
// 2 when neither of them is in use
func (h *TLSCertRoationReconciler) verifyCert(ip string, port int, newCert, currentCert string) (int, error) {
	conf := &tls.Config{
		InsecureSkipVerify: true,
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
