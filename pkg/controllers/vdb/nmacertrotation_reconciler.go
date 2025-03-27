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
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSServerCertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the https server.
type NMACertRoationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeNMACertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &NMACertRoationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("NMACertRoationReconciler"),
		Dispatcher: dispatcher,
		Pfacts:     pfacts,
	}
}

// Reconcile will rotate TLS certificate.
func (h *NMACertRoationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if vmeta.UseNMACertsMount(h.Vdb.Annotations) || !vmeta.EnableTLSCertsRotation(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}
	if !h.Vdb.IsStatusConditionTrue(vapi.HTTPSCertRotationFinished) || !h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) {
		return ctrl.Result{}, nil
	}
	curretSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret

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

	h.Log.Info("to start nma cert rotation")
	res, err := h.rotateNmaTLSCert(ctx, &newSecret, &currentSecret)

	cond := vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionFalse, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}
	cond = vapi.MakeCondition(vapi.HTTPSCertRotationFinished, metav1.ConditionFalse, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		return ctrl.Result{}, err
	}
	return res, err
}

// rotateHTTPSTLSCert will rotate node management agent's tls cert from currentSecret to newSecret
func (h *NMACertRoationReconciler) rotateNmaTLSCert(ctx context.Context, newSecret, currentSecret *corev1.Secret) (ctrl.Result, error) {
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
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationStarted,
		"Start rotating nma cert from %s to %s", currentSecretName, newSecretName)
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
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationSucceeded,
			"Successfully rotated nma cert from %s to %s", currentSecretName, newSecretName)
	}
	return result, err2
}

// checkCertAfterRoation will return different result and error based on result from calling verifyCert
func (h *NMACertRoationReconciler) checkCertAfterRoation(moduleName, ip string, port int, newCertName, newCert, currentCert string) (ctrl.Result, error) {
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
func (h *NMACertRoationReconciler) verifyCert(ip string, port int, newCert, currentCert string) (int, error) {
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
