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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/secrets"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSModeReconciler will update the tls modes when they are changed by users
type TLSModeReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

const (
	httpsTLSConfig = iota
	clientServerTLSConfig
)

func MakeTLSModeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSModeReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSModeReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will call cert rotation API to update the TLS mode of the https server
func (h *TLSModeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsTLSAuthEnabled() || h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) ||
		!h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) {
		return ctrl.Result{}, nil
	}
	if h.Vdb.Spec.HTTPSTLSMode == h.Vdb.GetHTTPSTLSModeInUse() &&
		h.Vdb.Spec.ClientServerTLSMode == h.Vdb.GetClientServerTLSModeInUse() {
		return ctrl.Result{}, nil
	}
	h.Log.Info(fmt.Sprintf("https: current tls mode - %s, spec tls mode - %s", h.Vdb.GetHTTPSTLSModeInUse(), h.Vdb.Spec.HTTPSTLSMode))
	h.Log.Info(fmt.Sprintf("client: current tls mode - %s, spec tls mode - %s", h.Vdb.GetClientServerTLSModeInUse(),
		h.Vdb.Spec.ClientServerTLSMode))
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSModeUpdateStarted,
		"Starting to update TLS Mode")
	if h.Vdb.GetHTTPSTLSModeInUse() == "" || h.Vdb.GetClientServerTLSModeInUse() == "" {
		res, err := h.reconcileAfterRevive(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		if h.Vdb.Spec.HTTPSTLSMode == h.Vdb.GetHTTPSTLSModeInUse() &&
			h.Vdb.Spec.ClientServerTLSMode == h.Vdb.GetClientServerTLSModeInUse() {
			return ctrl.Result{}, nil
		}
	}
	res, err := h.rotateTLSMode(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, nil
}

func (h *TLSModeReconciler) rotateTLSMode(ctx context.Context) (ctrl.Result, error) {
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run vsql to update tls mode. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	currentSecretName := h.Vdb.GetHTTPSTLSSecretNameInUse()
	nmCurrentSecretName := types.NamespacedName{
		Name:      currentSecretName,
		Namespace: h.Vdb.GetNamespace(),
	}
	secretFetcher := &cloud.SecretFetcher{
		Client:   h.VRec.Client,
		Log:      h.Log,
		EVWriter: h.VRec,
		Obj:      h.Vdb,
	}
	currentSecretData, res, err := secretFetcher.FetchAllowRequeue(ctx, nmCurrentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	currentSecret := &corev1.Secret{
		Data: currentSecretData,
	}
	currentHTTPSTLSMode := h.Vdb.GetHTTPSTLSModeInUse()
	newHTTPSTLSMode := h.Vdb.Spec.HTTPSTLSMode
	currentClientTLSMode := h.Vdb.GetClientServerTLSModeInUse()
	newClientTLSMode := h.Vdb.Spec.ClientServerTLSMode

	if currentHTTPSTLSMode != newHTTPSTLSMode {
		h.Log.Info(fmt.Sprintf("ready to change HTTPS TLS mode from %s to %s", currentHTTPSTLSMode, newHTTPSTLSMode))
	}
	if currentClientTLSMode != newClientTLSMode {
		h.Log.Info(fmt.Sprintf("ready to change client TLS mode from %s to %s", currentClientTLSMode, newClientTLSMode))
	}

	var keyConfig, certConfig, caCertConfig, secretName string
	switch {
	case secrets.IsAWSSecretsManagerSecret(h.Vdb.Spec.HTTPSNMATLSSecret):
		keyConfig, certConfig, caCertConfig = GetAWSCertsConfig(h.Vdb)
		secretName = secrets.RemovePathReference(h.Vdb.Spec.HTTPSNMATLSSecret)
	default:
		keyConfig, certConfig, caCertConfig = GetK8sCertsConfig(h.Vdb)
		secretName = h.Vdb.Spec.HTTPSNMATLSSecret
	}

	currentCert := string(currentSecret.Data[corev1.TLSCertKey])

	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(string(currentSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatehttpscerts.WithPollingCert(currentCert),
		rotatehttpscerts.WithPollingCaCert(string(currentSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatehttpscerts.WithKey(secretName, keyConfig),
		rotatehttpscerts.WithCert(secretName, certConfig),
		rotatehttpscerts.WithCaCert(secretName, caCertConfig),
		rotatehttpscerts.WithTLSMode(newHTTPSTLSMode),
		rotatehttpscerts.WithInitiator(initiatorPod.GetPodIP()),
	}
	h.Log.Info(fmt.Sprintf("call RotateHTTPSCerts for cert - %s , tls mode - %s", h.Vdb.Spec.HTTPSNMATLSSecret, newHTTPSTLSMode))
	err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate HTTPS/client TLS mode")
		return ctrl.Result{}, err
	}
	httpsTLSMode := vapi.MakeHTTPSTLSMode(h.Vdb.Spec.HTTPSTLSMode)
	clientTLSMode := vapi.MakeClientServerTLSMode(h.Vdb.Spec.ClientServerTLSMode)
	err = vdbstatus.UpdateTLSModes(ctx, h.VRec.Client, h.Vdb, []*vapi.TLSMode{httpsTLSMode, clientTLSMode})
	if err != nil {
		h.Log.Error(err, "failed to update tls mode after rotating tls mode")
		return ctrl.Result{}, err
	}

	h.Log.Info(fmt.Sprintf("HTTPS TLS mode is %s, client TLS mode is %s", newHTTPSTLSMode, newClientTLSMode))

	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSModeUpdateSucceeded,
		"Successfully updated TLS modes. https:  %s -> %s, client: %s -> %s",
		newHTTPSTLSMode, newClientTLSMode, currentHTTPSTLSMode, currentClientTLSMode)
	return ctrl.Result{}, nil
}

func (h *TLSModeReconciler) reconcileAfterRevive(ctx context.Context) (ctrl.Result, error) {
	requireUpdate := false
	configSet := []int{httpsTLSConfig, clientServerTLSConfig}
	for _, tlsConfig := range configSet {
		needUpdate, res, err := h.loadTLSModeAfterRevive(ctx, tlsConfig)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		if needUpdate {
			requireUpdate = true
		}
	}
	if requireUpdate {
		httpsTLSMode := vapi.MakeHTTPSTLSMode(h.Vdb.Spec.HTTPSTLSMode)
		clientTLSMode := vapi.MakeClientServerTLSMode(h.Vdb.Spec.ClientServerTLSMode)
		err := vdbstatus.UpdateTLSModes(ctx, h.VRec.Client, h.Vdb, []*vapi.TLSMode{httpsTLSMode, clientTLSMode})
		if err != nil {
			h.Log.Error(err, "failed to update tls mode after reviving")
			return ctrl.Result{}, err
		}
		h.Log.Info("tls modes retrieved from db are saved into vdb status")
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSModeUpdateSucceeded,
			"Successfully updated TLS modes after reviving. https - %s, client - %s", h.Vdb.Spec.HTTPSTLSMode,
			h.Vdb.Spec.ClientServerTLSMode)
	}
	return ctrl.Result{}, nil
}

func (h *TLSModeReconciler) loadTLSModeAfterRevive(ctx context.Context, tlsConfig int) (bool, ctrl.Result, error) {
	currentTLSMode, _ := h.getCurrentTLSMode(tlsConfig)
	newTLSMode, _ := h.getNewTLSMode(tlsConfig)
	h.Log.Info(fmt.Sprintf("starting to reconcile https tls mode, current TLS mode - %s, new TLS mode - %s", currentTLSMode, newTLSMode))
	if currentTLSMode != "" {
		return false, ctrl.Result{}, nil
	}
	tlsConfigName, _ := h.getTLSConfig(tlsConfig)
	h.Log.Info(fmt.Sprintf("set tls mode after reviving for %s", tlsConfigName))
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run sql to get tls mode. Requeue reconciliation.")
		return false, ctrl.Result{Requeue: true}, nil
	}
	sql := fmt.Sprintf("select mode from tls_configurations where name='%s';", tlsConfigName)
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	h.Log.Info(fmt.Sprintf("%s tls mode from db - %s", tlsConfigName, stdout))
	if err != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err, fmt.Sprintf("failed to retrieve %s TLS mode after reviving db, stderr - %s", tlsConfigName, stderr))
		return false, ctrl.Result{}, err
	}
	currentTLSMode = h.getTLSMode(stdout)
	_ = h.setNewTLSMode(tlsConfig, currentTLSMode)
	return true, ctrl.Result{}, nil
}

func (h *TLSModeReconciler) getTLSMode(stdout string) string {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res
}

func (h *TLSModeReconciler) getTLSConfig(tlsConfig int) (string, error) {
	switch tlsConfig {
	case httpsTLSConfig:
		return "https", nil
	case clientServerTLSConfig:
		return "server", nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}

func (h *TLSModeReconciler) getNewTLSMode(tlsConfig int) (string, error) {
	switch tlsConfig {
	case httpsTLSConfig:
		return h.Vdb.Spec.HTTPSTLSMode, nil
	case clientServerTLSConfig:
		return h.Vdb.Spec.ClientServerTLSMode, nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}

func (h *TLSModeReconciler) getCurrentTLSMode(tlsConfig int) (string, error) {
	switch tlsConfig {
	case httpsTLSConfig:
		return h.Vdb.GetHTTPSTLSModeInUse(), nil
	case clientServerTLSConfig:
		return h.Vdb.GetClientServerTLSModeInUse(), nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}

func (h *TLSModeReconciler) setNewTLSMode(tlsConfig int, currentTLSMode string) error {
	switch tlsConfig {
	case httpsTLSConfig:
		h.Vdb.Spec.HTTPSTLSMode = currentTLSMode
		return nil
	case clientServerTLSConfig:
		h.Vdb.Spec.ClientServerTLSMode = currentTLSMode
		return nil
	}
	return fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}
