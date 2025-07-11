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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertRotationReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for nma service. This
// happens after the cert rotation is successful for https service
type NMACertRotationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
	Manager    *TLSConfigManager
}

func MakeNMACertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &NMACertRotationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("NMACertRotationReconciler"),
		Dispatcher: dispatcher,
		Pfacts:     pfacts,
		Manager:    MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigHTTPS, dispatcher),
	}
}

// Reconcile will rotate TLS certificate.
func (h *NMACertRotationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsSetForTLS() {
		return ctrl.Result{}, nil
	}
	// no-op if tls update has not occurred
	if (!h.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) &&
		!h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSConfigUpdateFinished)) ||
		!h.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) ||
		h.Vdb.IsTLSCertRollbackNeeded() {
		return ctrl.Result{}, nil
	}

	// we want to be sure nma tls configmap exists and has the freshest values
	if res, errCheck := h.Manager.checkNMATLSConfigMap(ctx); verrors.IsReconcileAborted(res, errCheck) {
		return res, errCheck
	}

	// nma secret
	newSecretName := h.Vdb.GetHTTPSNMATLSSecret()

	newSecret, res, err := readSecret(h.Vdb, h.VRec, h.VRec.GetClient(), h.Log, ctx, newSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	h.Log.Info("Starting NMA TLS certificate rotation")
	err, calledVclusterops := h.rotateNmaTLSCert(ctx, newSecret)
	if err != nil {
		h.Log.Error(err, "Failed to rotate NMA TLS certificate")
		if calledVclusterops {
			cond := vapi.MakeCondition(vapi.TLSCertRollbackNeeded, metav1.ConditionTrue, vapi.RollbackAfterNMACertRotationReason)
			return res, vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond)
		}
		return res, err
	}

	updateConds := func(conds []*metav1.Condition) error {
		if err := vdbstatus.UpdateConditions(ctx, h.VRec.GetClient(), h.Vdb, conds); err != nil {
			return err
		}
		return nil
	}

	conds := []*metav1.Condition{
		// Clear TLSConfigUpdateInProgress condition
		vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionFalse, "Completed"),
	}

	if h.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) {
		conds = append(conds, vapi.MakeCondition(vapi.HTTPSTLSConfigUpdateFinished, metav1.ConditionFalse, "Completed"))
	}

	// client server does not need nma cert rotation but it needs nma restart
	// so the container can get the latest client-server tls mode. We will
	// improve this later so that client server does not need nma cert rotation
	// by updating the mode only when client server tls config update
	// was done successfully.
	if h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSConfigUpdateFinished) {
		conds = append(conds, vapi.MakeCondition(vapi.ClientServerTLSConfigUpdateFinished, metav1.ConditionFalse, "Completed"))
	}

	return ctrl.Result{}, updateConds(conds)
}

// rotateHTTPSTLSCert will rotate node management agent's tls cert from currentSecret to newSecret
func (h *NMACertRotationReconciler) rotateNmaTLSCert(ctx context.Context, newSecret map[string][]byte) (error, bool) {
	err := h.Pfacts.Collect(ctx, h.Vdb)
	if err != nil {
		h.Log.Error(err, "nma cert rotation aborted. Failed to collect pod facts ")
		return err, false
	}

	var tls *vapi.TLSConfigStatus
	currentSecretName := h.Vdb.GetHTTPSNMATLSSecretInUse()
	newSecretName := h.Vdb.GetHTTPSNMATLSSecret()

	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationStarted,
		"Start rotating nma cert from %s to %s", currentSecretName, newSecretName)
	h.Log.Info("Rotating NMA certificate", "from", currentSecretName, "to", newSecretName,
		"tlsEnabled", h.Vdb.IsSetForTLS(),
	)

	hosts := []string{}
	for _, detail := range h.Pfacts.Detail {
		hosts = append(hosts, detail.GetPodIP())
	}

	opts := []rotatenmacerts.Option{
		rotatenmacerts.WithKey(string(newSecret[corev1.TLSPrivateKeyKey])),
		rotatenmacerts.WithCert(string(newSecret[corev1.TLSCertKey])),
		rotatenmacerts.WithCaCert(string(newSecret[corev1.ServiceAccountRootCAKey])),
		rotatenmacerts.WithHosts(hosts),
	}
	err = h.Dispatcher.RotateNMACerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate nma cer to "+newSecretName)
		return err, true
	}

	if h.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) {
		tls = vapi.MakeHTTPSNMATLSConfig(h.Vdb.GetHTTPSNMATLSSecret(), h.Vdb.GetHTTPSNMATLSMode())
		if updErr := vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSConfigStatus{tls}); updErr != nil {
			return err, true
		}
	}

	if h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSConfigUpdateFinished) {
		tls = vapi.MakeClientServerTLSConfig(h.Vdb.GetClientServerTLSSecret(), h.Vdb.GetClientServerTLSMode())
		if updErr := vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSConfigStatus{tls}); updErr != nil {
			return err, true
		}
	}

	h.Log.Info("saved new tls cert secret name in status", "secret", newSecretName)
	// last thing is to update vdb condition
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSCertRotationSucceeded,
		"Successfully rotated nma cert from %s to %s", currentSecretName, newSecretName)

	return err, true
}
