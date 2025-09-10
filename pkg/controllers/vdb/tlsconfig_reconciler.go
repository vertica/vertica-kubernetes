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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSConfigReconciler will turn on the tls config when users request it
type TLSConfigReconciler struct {
	VRec          *VerticaDBReconciler
	Vdb           *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log           logr.Logger
	PRunner       cmds.PodRunner
	Dispatcher    vadmin.Dispatcher
	Pfacts        *podfacts.PodFacts
	TLSConfigName string
	Manager       *TLSConfigManager
}

func MakeTLSConfigReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts, tlsConfigName string, manager *TLSConfigManager) controllers.ReconcileActor {
	return &TLSConfigReconciler{
		VRec:          vdbrecon,
		Vdb:           vdb,
		Log:           log.WithName("TLSConfigReconciler"),
		Dispatcher:    dispatcher,
		PRunner:       prunner,
		Pfacts:        pfacts,
		TLSConfigName: tlsConfigName,
		Manager:       manager,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if h.Vdb.IsSetForTLS() && h.Vdb.GetSecretInUse(h.TLSConfigName) != "" ||
		!h.Vdb.IsSetForTLS() || !h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) ||
		h.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress) ||
		h.Vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded) {
		return ctrl.Result{}, nil
	}

	h.Log.Info("Starting TLS reconciliation",
		"certRotationEnabled", h.Vdb.IsSetForTLS(),
		"secretName", h.Vdb.GetSecretInUse(h.TLSConfigName),
		"dbInitialized", h.Vdb.IsStatusConditionTrue(vapi.DBInitialized),
	)

	err := h.Pfacts.Collect(ctx, h.Vdb)
	if err != nil {
		h.Log.Error(err, "failed to collect pfacts to set up tls. skip current loop")
		return ctrl.Result{}, nil
	}

	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to set up tls config. restart next.")
		restartReconciler := MakeRestartReconciler(h.VRec, h.Log, h.Vdb, h.PRunner, h.Pfacts, true, h.Dispatcher)
		res, err2 := restartReconciler.Reconcile(ctx, request)
		return res, err2
	}

	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationStarted,
		"Starting to configure TLS for %s", h.TLSConfigName)

	configured, tlsMode, err := h.checkIfTLSConfiguredInDB(ctx, initiatorPod)
	if err != nil {
		h.Log.Error(err, "failed to check TLS configuration before setting up TLS")
		return ctrl.Result{}, err
	}

	if !configured {
		authCreated, err2 := h.checkIfTLSAuthenticationCreatedInDB(ctx, initiatorPod)
		if err2 != nil {
			h.Log.Error(err2, "failed to check TLS authentication before running DDL")
			return ctrl.Result{}, err2
		}
		h.Log.Info("Run DDL to set up TLS")
		err = h.runDDLToConfigureTLS(ctx, initiatorPod, !authCreated)
		if err != nil {
			h.VRec.Eventf(h.Vdb, corev1.EventTypeWarning, events.TLSConfigurationFailed,
				"Failed to set %s tls config with secret name %s and mode %s", h.Manager.TLSConfig, h.Manager.NewSecret, tlsMode)
			return ctrl.Result{}, err
		}
		tlsMode = h.Manager.NewTLSMode
	} else {
		h.Log.Info("TLS already configured in db. Skip running DDL.")
	}

	h.Log.Info("Saving TLS secret and mode into status")
	err = h.Manager.setTLSConfigInStatus(ctx, strings.ToLower(tlsMode))
	if err != nil {
		h.Log.Error(err, "failed to save TLS secret and mode into status")
		return ctrl.Result{}, err
	}

	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationSucceeded,
		"Successfully set %s tls config", h.Manager.TLSConfig)

	return ctrl.Result{}, nil
}

func (h *TLSConfigReconciler) runDDLToConfigureTLS(ctx context.Context, initiatorPod *podfacts.PodFact, grantAuth bool) error {
	return h.Manager.setTLSConfigInDB(ctx, initiatorPod, grantAuth)
}

func (h *TLSConfigReconciler) checkIfTLSConfiguredInDB(ctx context.Context,
	initiatorPod *podfacts.PodFact) (configured bool, tlsMode string, err error) {
	var certificate string
	certificate, tlsMode, err = h.Manager.getTLSConfigFromDB(ctx, h.Pfacts, initiatorPod)
	if err != nil {
		return
	}
	configured = strings.Contains(certificate, h.Manager.getCertificatePrefix())
	return
}

func (h *TLSConfigReconciler) checkIfTLSAuthenticationCreatedInDB(ctx context.Context, initiatorPod *podfacts.PodFact) (bool, error) {
	sql := "select is_auth_enabled from client_auth where auth_name='k8s_remote_ipv4_tls_builtin_auth';"
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if err != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err, fmt.Sprintf("failed to check if TLS authentication is configured, stderr - %s", stderr))
		return false, err
	}
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res == "True", nil
}
