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
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

// GetTLSModeAfterReviveReconciler gets the tls modes from the db
// and cache them in the status. This is for a db that have been revived
// from a db with tls config set.
type GetTLSModeAfterReviveReconciler struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log     logr.Logger
	PRunner cmds.PodRunner
	Pfacts  *podfacts.PodFacts
}

func MakeGetTLSModeAfterReviveReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &GetTLSModeAfterReviveReconciler{
		VRec:    vdbrecon,
		Vdb:     vdb,
		Log:     log.WithName("GetTLSModeAfterReviveReconciler"),
		PRunner: prunner,
		Pfacts:  pfacts,
	}
}

func (h *GetTLSModeAfterReviveReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Skip this reconciler entirely if the init policy is to create the DB.
	if h.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyRevive {
		return ctrl.Result{}, nil
	}

	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}

	if h.Vdb.GetHTTPSTLSModeInUse() != "" && h.Vdb.GetClientServerTLSModeInUse() != "" {
		// no-op tls modes already in status
		return ctrl.Result{}, nil
	}

	if err := h.Pfacts.Collect(ctx, h.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	configSet := []int{httpsTLSConfig, clientServerTLSConfig}
	tlsConfigs := []*vapi.TLSConfig{}
	for _, tlsConfig := range configSet {
		mode, res, err := h.getTLSModeAfterRevive(ctx, tlsConfig)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
		if mode == "" {
			// no tls config found
			continue
		}
		if tlsConfig == httpsTLSConfig {
			tlsConfigs = append(tlsConfigs, vapi.MakeHTTPSNMATLSConfig(h.Vdb.Spec.HTTPSNMATLS.Secret, mode))
		} else {
			tlsConfigs = append(tlsConfigs, vapi.MakeClientServerTLSConfig(h.Vdb.Spec.ClientServerTLS.Secret, mode))
		}
	}
	if len(tlsConfigs) > 0 {
		err := vdbstatus.UpdateTLSConfigs(ctx, h.VRec.Client, h.Vdb, tlsConfigs)
		if err != nil {
			h.Log.Error(err, "failed to update tls configs after reviving")
			return ctrl.Result{}, err
		}
		h.Log.Info("Successfully updated TLS Configs in status after reviving", "tlsConfigs", tlsConfigs)
	}
	return ctrl.Result{}, nil
}

// getTLSModeAfterRevive get tls mode from the db for the given tls config
func (h *GetTLSModeAfterReviveReconciler) getTLSModeAfterRevive(ctx context.Context, tlsConfig int) (string, ctrl.Result, error) {
	tlsModeInStatus, _ := h.getCurrentTLSMode(tlsConfig)
	if tlsModeInStatus != "" {
		return "", ctrl.Result{}, nil
	}
	tlsConfigName, _ := h.getTLSConfig(tlsConfig)
	h.Log.Info(fmt.Sprintf("Get tls mode after reviving for %s", tlsConfigName))
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run sql to get tls mode. Requeue reconciliation.")
		return "", ctrl.Result{Requeue: true}, nil
	}
	sql := fmt.Sprintf("select mode from tls_configurations where name='%s';", tlsConfigName)
	cmd := []string{"-tAc", sql}
	stdout, stderr, err := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	h.Log.Info(fmt.Sprintf("%s tls mode from db - %s", tlsConfigName, stdout))
	if err != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err, fmt.Sprintf("failed to retrieve %s TLS mode after reviving db, stderr - %s", tlsConfigName, stderr))
		return "", ctrl.Result{}, err
	}

	return h.getTLSMode(stdout), ctrl.Result{}, nil
}

// getTLSMode parses and return the tls mode
func (h *GetTLSModeAfterReviveReconciler) getTLSMode(stdout string) string {
	lines := strings.Split(stdout, "\n")
	res := strings.Trim(lines[0], " ")
	return res
}

func (h *GetTLSModeAfterReviveReconciler) getTLSConfig(tlsConfig int) (string, error) {
	const server = "server"
	switch tlsConfig {
	case httpsTLSConfig:
		return "https", nil
	case clientServerTLSConfig:
		return server, nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}

func (h *GetTLSModeAfterReviveReconciler) getCurrentTLSMode(tlsConfig int) (string, error) {
	switch tlsConfig {
	case httpsTLSConfig:
		return h.Vdb.GetHTTPSTLSModeInUse(), nil
	case clientServerTLSConfig:
		return h.Vdb.GetClientServerTLSModeInUse(), nil
	}
	return "", fmt.Errorf("invalid tlsConfig %d", tlsConfig)
}
