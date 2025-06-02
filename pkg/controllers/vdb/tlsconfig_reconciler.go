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
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSConfigReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSConfigReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSConfigReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSConfigReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	h.Log.Info("in tls config reconcile 1, enabled ? " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
	if h.Vdb.IsCertRotationEnabled() && len(h.Vdb.Status.SecretRefs) != 0 ||
		!h.Vdb.IsCertRotationEnabled() || !h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) ||
		h.Vdb.IsStatusConditionTrue(vapi.UpgradeInProgress) ||
		h.Vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded) {
		return ctrl.Result{}, nil
	}

	h.Log.Info("entry condition, cert rotate enabled ? " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()) +
		", num of status secrets - " + strconv.Itoa(len(h.Vdb.Status.SecretRefs)) + ", is db initialized ? " +
		strconv.FormatBool(h.Vdb.IsStatusConditionTrue(vapi.DBInitialized)) + ", setup tls - ")
	h.Log.Info("tls enabled, start to set up tls config")
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
		"Starting to configure TLS")

	configured, err := h.checkIfTLSConfiguredInDB(ctx, initiatorPod)
	if err != nil {
		h.Log.Error(err, "failed to check TLS configuration before setting up TLS")
		return ctrl.Result{}, err
	}
	h.Log.Info("restarted nma before setting up tls config")

	if !configured {
		h.Log.Info("run DDL to set up TLS")
		err = h.runDDLToConfigureTLS(ctx, initiatorPod)
		if err != nil {
			return ctrl.Result{}, err
		}
		h.Log.Info("executed DDL to set up TLS")
	} else {
		h.Log.Info("TLS already configured in db. Skip running DDL.")
	}
	err = h.updateStatus(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info("saved TLS secrets and modes into status")
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSConfigurationSucceeded,
		"Successfully configured TLS")
	return ctrl.Result{}, nil
}

func (h *TLSConfigReconciler) runDDLToConfigureTLS(ctx context.Context, initiatorPod *podfacts.PodFact) error {
	opts := []settlsconfig.Option{
		settlsconfig.WithHTTPSTLSMode(h.Vdb.Spec.HTTPSTLSMode),
		settlsconfig.WithHTTPSTLSSecretName(h.Vdb.Spec.HTTPSNMATLSSecret),
		settlsconfig.WithClientServerTLSMode(h.Vdb.Spec.ClientServerTLSMode),
		settlsconfig.WithClientServerTLSSecretName(h.Vdb.Spec.ClientServerTLSSecret),
		settlsconfig.WithInitiatorIP(initiatorPod.GetPodIP()),
		settlsconfig.WithNamespace(h.Vdb.GetObjectMeta().GetNamespace()),
	}
	return h.Dispatcher.SetTLSConfig(ctx, opts...)
}

func (h *TLSConfigReconciler) updateStatus(ctx context.Context) error {
	sec := vapi.MakeHTTPSTLSSecretRef(h.Vdb.Spec.HTTPSNMATLSSecret)
	if err1 := vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, sec); err1 != nil {
		return err1
	}
	clientSec := vapi.MakeClientServerTLSSecretRef(h.Vdb.Spec.ClientServerTLSSecret)
	if err3 := vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, clientSec); err3 != nil {
		return err3
	}
	sRefs := []*vapi.SecretRef{
		sec, clientSec,
	}
	if err2 := vdbstatus.UpdateSecretRefs(ctx, h.VRec.GetClient(), h.Vdb, sRefs); err2 != nil {
		return err2
	}
	h.Log.Info("saved secrets into status")
	httpsTLSMode := vapi.MakeHTTPSTLSMode(h.Vdb.Spec.HTTPSTLSMode)
	clientTLSMode := vapi.MakeClientServerTLSMode(h.Vdb.Spec.ClientServerTLSMode)
	err := vdbstatus.UpdateTLSModes(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSMode{httpsTLSMode, clientTLSMode})
	if err != nil {
		h.Log.Error(err, "failed to update tls mode when setting up TLS")
		return err
	}
	return nil
}

func (h *TLSConfigReconciler) checkIfTLSConfiguredInDB(ctx context.Context, initiatorPod *podfacts.PodFact) (bool, error) {
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
