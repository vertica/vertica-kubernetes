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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/settlsconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
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
}

func MakeTLSConfigReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts, tlsConfigName string) controllers.ReconcileActor {
	return &TLSConfigReconciler{
		VRec:          vdbrecon,
		Vdb:           vdb,
		Log:           log.WithName("TLSConfigReconciler"),
		Dispatcher:    dispatcher,
		PRunner:       prunner,
		Pfacts:        pfacts,
		TLSConfigName: tlsConfigName,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if h.Vdb.IsTLSAuthEnabled() && h.Vdb.GetSecretInUse(h.TLSConfigName) != "" ||
		!h.Vdb.IsTLSAuthEnabled() || !h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) ||
		h.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress) ||
		h.Vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded) {
		return ctrl.Result{}, nil
	}
	h.Log.Info("entry condition, cert rotate enabled ? " + strconv.FormatBool(h.Vdb.IsTLSAuthEnabled()) +
		", status secret name - " + h.Vdb.GetSecretInUse(h.TLSConfigName) + ", is db initialized ? " +
		strconv.FormatBool(h.Vdb.IsStatusConditionTrue(vapi.DBInitialized)))
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

	configured, err := h.checkIfTLSConfiguredInDB(ctx, initiatorPod)
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
			h.Log.Error(err, "failed to run DDL to set up TLS")
			return ctrl.Result{}, err
		}
	} else {
		h.Log.Info("TLS already configured in db. Skip running DDL.")
	}
	h.Log.Info("Save TLS secret and mode into status")
	err = h.updateStatus(ctx, initiatorPod, configured)
	if err != nil {
		h.Log.Error(err, "failed to save TLS secret and mode into status")
		return ctrl.Result{}, err
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationSucceeded, "Successfully configured TLS for %s", h.TLSConfigName)
	return ctrl.Result{}, nil
}

func (h *TLSConfigReconciler) runDDLToConfigureTLS(ctx context.Context, initiatorPod *podfacts.PodFact, grantAuth bool) error {
	var opts []settlsconfig.Option
	if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
		opts = []settlsconfig.Option{
			settlsconfig.WithHTTPSTLSMode(h.Vdb.GetHTTPSNMATLSMode()),
			settlsconfig.WithHTTPSTLSSecretName(h.Vdb.GetHTTPSNMATLSSecret()),
			settlsconfig.WithInitiatorIP(initiatorPod.GetPodIP()),
			settlsconfig.WithNamespace(h.Vdb.GetObjectMeta().GetNamespace()),
			settlsconfig.WithHTTPSTLSConfig(true),
			settlsconfig.WithGrantAuth(grantAuth),
		}
	} else {
		opts = []settlsconfig.Option{
			settlsconfig.WithClientServerTLSMode(h.Vdb.GetClientServerTLSMode()),
			settlsconfig.WithClientServerTLSSecretName(h.Vdb.GetClientServerTLSMode()),
			settlsconfig.WithInitiatorIP(initiatorPod.GetPodIP()),
			settlsconfig.WithNamespace(h.Vdb.GetObjectMeta().GetNamespace()),
			settlsconfig.WithHTTPSTLSConfig(false),
			settlsconfig.WithGrantAuth(grantAuth),
		}
	}
	return h.Dispatcher.SetTLSConfig(ctx, opts...)
}

func (h *TLSConfigReconciler) updateStatus(ctx context.Context, initiatorPod *podfacts.PodFact, tlsConfiguredInDB bool) error {
	err := h.updateSecretsInStatus(ctx)
	if err != nil {
		return err
	}
	err = h.updateTLSModesInStatus(ctx, initiatorPod, tlsConfiguredInDB)
	if err != nil {
		return err
	}
	return nil
}

func (h *TLSConfigReconciler) updateSecretsInStatus(ctx context.Context) error {
	var tlsConfig *vapi.TLSConfigStatus
	if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
		tlsConfig = vapi.MakeHTTPSNMATLSConfigFromSpec(h.Vdb.Spec.HTTPSNMATLS)
	} else {
		tlsConfig = vapi.MakeClientServerTLSConfigFromSpec(h.Vdb.Spec.ClientServerTLS)
	}
	if err := vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSConfigStatus{tlsConfig}); err != nil {
		return err
	}
	h.Log.Info(fmt.Sprintf("Save secret %s into status", tlsConfig.Secret))
	return nil
}

func (h *TLSConfigReconciler) updateTLSModesInStatus(ctx context.Context, initiatorPod *podfacts.PodFact, tlsConfiguredInDB bool) error {
	var tlsModeStr string
	err := error(nil)
	if tlsConfiguredInDB {
		tlsModeStr, err = h.loadTLSModeFromDB(ctx, initiatorPod)
		if err != nil {
			return err
		}
	} else {
		if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
			tlsModeStr = h.Vdb.GetHTTPSNMATLSMode()
		} else {
			tlsModeStr = h.Vdb.GetClientServerTLSMode()
		}
	}
	var tlsConfig *vapi.TLSConfigStatus
	if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
		tlsConfig = vapi.MakeHTTPSNMATLSConfig(h.Vdb.GetHTTPSNMATLSSecret(), tlsModeStr)
	} else {
		tlsConfig = vapi.MakeClientServerTLSConfig(h.Vdb.GetClientServerTLSMode(), tlsModeStr)
	}
	tlsConfigs := []*vapi.TLSConfigStatus{tlsConfig}
	err = vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, tlsConfigs)
	if err != nil {
		h.Log.Error(err, "failed to update tls mode when setting up TLS")
		return err
	}
	h.Log.Info(fmt.Sprintf("tls mode %s has been saved into vdb status for secret type %s", tlsModeStr, h.TLSConfigName))
	return nil
}

func (h *TLSConfigReconciler) checkIfTLSConfiguredInDB(ctx context.Context, initiatorPod *podfacts.PodFact) (bool, error) {
	var sql string
	if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
		sql = "select certificate from tls_configurations where name='https';"
	} else {
		sql = "select certificate from tls_configurations where name='server';"
	}
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if err != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err, fmt.Sprintf("failed to check if TLS is configured for %s, stderr - %s", h.TLSConfigName, stderr))
		return false, err
	}
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res != "httpServerCert", nil
}

func (h *TLSConfigReconciler) loadTLSModeFromDB(ctx context.Context, initiatorPod *podfacts.PodFact) (string, error) {
	var tlsConfigName string
	if h.TLSConfigName == vapi.HTTPSNMATLSConfigName {
		tlsConfigName = "https"
	} else {
		tlsConfigName = "server"
	}
	h.Log.Info(fmt.Sprintf("read tls mode from db for %s", tlsConfigName))
	sql := fmt.Sprintf("select mode from tls_configurations where name='%s';", tlsConfigName)
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	h.Log.Info(fmt.Sprintf("%s tls mode from db - %s", tlsConfigName, stdout))
	if err != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err, fmt.Sprintf("failed to retrieve %s TLS mode after reviving db, stderr - %s", tlsConfigName, stderr))
		return "", err
	}
	currentTLSMode := h.getTLSMode(stdout)
	return currentTLSMode, nil
}

func (h *TLSConfigReconciler) getTLSMode(stdout string) string {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res
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
