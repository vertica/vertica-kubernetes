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
	"strconv"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/security"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertRoationReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for nma service. This
// happens after the cert rotation is successful for https service
type NMACertRoationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeNMACertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
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
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}
	if !h.Vdb.IsStatusConditionTrue(vapi.HTTPSCertRotationFinished) || !h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) {
		return ctrl.Result{}, nil
	}
	currentSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret

	currentSecret, newSecret, res, err := readSecretsAndConfigMap(true, h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx,
		currentSecretName, newSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	h.Log.Info("to start nma cert rotation")
	res, err2 := h.rotateNmaTLSCert(ctx, newSecret, currentSecret)
	if err2 == nil {
		cond := vapi.MakeCondition(vapi.TLSCertRotationInProgress, metav1.ConditionFalse, "Completed")
		if err3 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err3 != nil {
			h.Log.Error(err3, "failed to reset condition \"TLSCertRotationInProgress\"")
			return ctrl.Result{}, err3
		}
		cond = vapi.MakeCondition(vapi.HTTPSCertRotationFinished, metav1.ConditionFalse, "Completed")
		if err4 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err4 != nil {
			h.Log.Error(err4, "failed to reset condition \"HTTPSCertRotationFinished\"")
			return ctrl.Result{}, err4
		}
	} else {
		h.Log.Error(err2, "failed to rotate nma cert")
	}
	return res, err2
}

// rotateHTTPSTLSCert will rotate node management agent's tls cert from currentSecret to newSecret
func (h *NMACertRoationReconciler) rotateNmaTLSCert(ctx context.Context, newSecret, currentSecret *corev1.Secret) (ctrl.Result, error) {
	err := h.Pfacts.Collect(ctx, h.Vdb)
	if err != nil {
		h.Log.Error(err, "nma cert rotation aborted. Failed to collect pod facts ")
		return ctrl.Result{}, err
	}
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run rotate nma cert. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	currentSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	newSecretName := h.Vdb.Spec.NMATLSSecret

	newCert := string(newSecret.Data[corev1.TLSCertKey])
	currentCert := string(currentSecret.Data[corev1.TLSCertKey])
	rotated, err := security.VerifyCert(initiatorPod.GetPodIP(), builder.NMAPort, newCert, currentCert, h.Log)
	if err != nil {
		h.Log.Error(err, "nma cert rotation aborted. Failed to verify new nma cert for "+
			initiatorPod.GetPodIP())
		return ctrl.Result{}, err
	}
	if rotated == 2 {
		h.Log.Info("nma cert rotation skipped. Neither new nor existing nma cert is in use on " +
			initiatorPod.GetPodIP())
		return ctrl.Result{Requeue: true}, nil
	}
	if rotated == 0 {
		h.Log.Info("nma cert rotation skipped. new nma cert for " +
			" is already in use on " + initiatorPod.GetPodIP())
	} else {
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationStarted,
			"Start rotating nma cert from %s to %s", currentSecretName, newSecretName)
		h.Log.Info("to rotate nma certi from " + currentSecretName + " to " + newSecretName +
			", tls enabled " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
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
		err = h.Dispatcher.RotateNMACerts(ctx, opts...)
		if err != nil {
			h.Log.Error(err, "failed to rotate nma cer to "+newSecretName)
			return ctrl.Result{}, err
		}
	}
	nmVdbName := types.NamespacedName{
		Name:      h.Vdb.Name,
		Namespace: h.Vdb.GetNamespace(),
	}
	updated, err := vk8s.UpdateAnnotation(vmeta.NMAHTTPSPreviousSecret, newSecretName, h.Vdb, ctx, h.VRec.Client, nmVdbName)
	if !updated {
		h.Log.Error(err, "failed to save new tls cert secret name in annotation after cert rotation")
		return ctrl.Result{}, err
	}
	h.Log.Info("saved new tls cert secret name " + newSecretName + " in annotation")
	// last thing is to update vdb condition
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationSucceeded,
		"Successfully rotated nma cert from %s to %s", currentSecretName, newSecretName)

	return ctrl.Result{}, err
}
