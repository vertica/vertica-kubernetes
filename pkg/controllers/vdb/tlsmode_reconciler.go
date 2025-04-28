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
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
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

/* type TLSMode struct {
	GetCurrentTLSMode	func(*vapi.VerticaDB) string
	GetNewTLSMode	func(*vapi.VerticaDB) string
	TLSConfigName   string
}*/

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

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSModeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	h.Log.Info("in tls mode reconcile")
	if !h.Vdb.IsCertRotationEnabled() || h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) ||
		!h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) {
		return ctrl.Result{}, nil
	}
	if h.Vdb.Spec.HTTPSTLSMode == vmeta.GetNMAHTTPSPreviousTLSMode(h.Vdb.Annotations) &&
		h.Vdb.Spec.ClientServerTLSMode == vmeta.GetClientServerPreviousTLSMode(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSModeUpdateStarted,
		"Starting to update TLS Mode")
	res, err := h.reconcileAfterRevive(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	res, err = h.rotateTLSMode(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	return res, err
}

func (h *TLSModeReconciler) rotateTLSMode(ctx context.Context) (ctrl.Result, error) {
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run vsql to update tls mode. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	currentSecretName := h.Vdb.GetNMATLSSecretNameInUse()
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
	currentHTTPSTLSMode := vmeta.GetNMAHTTPSPreviousTLSMode(h.Vdb.Annotations)
	newHTTPSTLSMode := h.Vdb.Spec.HTTPSTLSMode
	currentClientTLSMode := vmeta.GetClientServerPreviousTLSMode(h.Vdb.Annotations)
	newClientTLSMode := h.Vdb.Spec.ClientServerTLSMode

	if currentHTTPSTLSMode != newHTTPSTLSMode {
		h.Log.Info(fmt.Sprintf("ready to change HTTPS TLS mode from %s to %s", currentHTTPSTLSMode, newHTTPSTLSMode))
	}
	if currentClientTLSMode != newClientTLSMode {
		h.Log.Info(fmt.Sprintf("ready to change client TLS mode from %s to %s", currentClientTLSMode, newClientTLSMode))
	}

	currentCert := string(currentSecret.Data[corev1.TLSCertKey])
	keyConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, h.Vdb.Namespace)
	certConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, h.Vdb.Namespace)
	caCertConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, h.Vdb.Namespace)
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(string(currentSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatehttpscerts.WithPollingCert(currentCert),
		rotatehttpscerts.WithPollingCaCert(string(currentSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatehttpscerts.WithKey(h.Vdb.Spec.NMATLSSecret, keyConfig),
		rotatehttpscerts.WithCert(h.Vdb.Spec.NMATLSSecret, certConfig),
		rotatehttpscerts.WithCaCert(h.Vdb.Spec.NMATLSSecret, caCertConfig),
		rotatehttpscerts.WithTLSMode(newHTTPSTLSMode),
		rotatehttpscerts.WithInitiator(initiatorPod.GetPodIP()),
	}
	h.Log.Info("call RotateHTTPSCerts for cert - " + h.Vdb.Spec.NMATLSSecret + ", tls mode - " + newHTTPSTLSMode +
		", tls enabled " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
	err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate HTTPS/client TLS mode")
		return ctrl.Result{}, err
	}

	chgs := vk8s.MetaChanges{
		NewAnnotations: map[string]string{
			vmeta.NMAHTTPSPreviousTLSMode:     newHTTPSTLSMode,
			vmeta.ClientServerPreviousTLSMode: newClientTLSMode,
		},
	}
	if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info(fmt.Sprintf("HTTPS TLS mode is %s, client TLS mode is %s", newHTTPSTLSMode, newClientTLSMode))
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSModeUpdateSucceeded,
		"Successfully updated TLS Mode. HTTPS TLS mode is %s, client TLS mode is %s", newHTTPSTLSMode, newClientTLSMode)
	return ctrl.Result{}, nil
}

func (h *TLSModeReconciler) reconcileAfterRevive(ctx context.Context) (ctrl.Result, error) {
	requireUpdate := false
	configSet := []int{httpsTLSConfig, clientServerTLSConfig}
	var newAnnotations map[string]string
	for _, tlsConfig := range configSet {
		needUpdate, res, err := h.loadTLSModeAfterRevive(ctx, tlsConfig, newAnnotations)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		if needUpdate {
			requireUpdate = true
		}
	}
	if requireUpdate {
		err := h.VRec.Client.Update(ctx, h.Vdb)
		if err != nil {
			h.Log.Error(err, "failed to update https and client server tls modes for vdb")
			return ctrl.Result{}, err
		}
		h.Log.Info("tls modes retrieved from db are saved into vdb spec")
		chgs := vk8s.MetaChanges{
			NewAnnotations: map[string]string{
				vmeta.NMAHTTPSPreviousTLSMode:     h.Vdb.Spec.HTTPSTLSMode,
				vmeta.ClientServerPreviousTLSMode: h.Vdb.Spec.ClientServerTLSMode,
			},
		}
		if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {

		}
		h.Log.Info("tls modes retrieved from db are saved into vdb annotations")
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSModeUpdateSucceeded,
			"Successfully updated HTTPS TLS mode to %s, client server TLS mode to %s", h.Vdb.Spec.HTTPSTLSMode,
			h.Vdb.Spec.ClientServerTLSMode)
	}
	return ctrl.Result{}, nil
}

func (h *TLSModeReconciler) loadTLSModeAfterRevive(ctx context.Context, tlsConfig int, newAnnotations map[string]string) (bool, ctrl.Result, error) {
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
	h.setCurrentTLSMode(tlsConfig, currentTLSMode, newAnnotations)
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
		return vmeta.GetNMAHTTPSPreviousTLSMode(h.Vdb.Annotations), nil
	case clientServerTLSConfig:
		return vmeta.GetClientServerPreviousTLSMode(h.Vdb.Annotations), nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}

func (h *TLSModeReconciler) setCurrentTLSMode(tlsConfig int, currentTLSMode string, newAnnotations map[string]string) error {
	switch tlsConfig {
	case httpsTLSConfig:
		h.Vdb.Spec.HTTPSTLSMode = currentTLSMode
		newAnnotations[vmeta.NMAHTTPSPreviousTLSMode] = currentTLSMode
		return nil
	case clientServerTLSConfig:
		h.Vdb.Spec.ClientServerTLSMode = currentTLSMode
		newAnnotations[vmeta.ClientServerPreviousTLSMode] = currentTLSMode
		return nil
	}
	return fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}
