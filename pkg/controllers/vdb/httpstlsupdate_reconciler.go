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
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// HTTPSTLSUpdateReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for https service

const (
	noTLSChange = iota
	tlsModeChangeOnly
	httpsCertChangeOnly
	tlsModeAndCertChange
)

type HTTPSTLSUpdateReconciler struct {
	VRec         *VerticaDBReconciler
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log          logr.Logger
	Dispatcher   vadmin.Dispatcher
	PFacts       *podfacts.PodFacts
	Manager      *TLSConfigManager
	FromRollback bool // Whether or not this has been called from the rollback reconciler
}

func MakeHTTPSTLSUpdateReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts, fromRollback bool) controllers.ReconcileActor {
	return &HTTPSTLSUpdateReconciler{
		VRec:         vdbrecon,
		Vdb:          vdb,
		Log:          log.WithName("HTTPSTLSUpdateReconciler"),
		Dispatcher:   dispatcher,
		PFacts:       pfacts,
		Manager:      MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigHTTPS, dispatcher),
		FromRollback: fromRollback,
	}
}

// Reconcile will rotate TLS certificate.
func (h *HTTPSTLSUpdateReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// Skip if TLS not enabled, DB not initialized, or rotate has failed.
	// However, if called from rollback reconciler, always run.
	if h.Vdb.ShouldSkipTLSUpdateReconcile() && !h.FromRollback {
		return ctrl.Result{}, nil
	}

	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// initialize the manager
	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()

	if h.Vdb.GetHTTPSNMATLSSecretInUse() == "" {
		rec := MakeTLSConfigReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.Dispatcher, h.PFacts, vapi.HTTPSNMATLSConfigName, h.Manager)
		return rec.Reconcile(ctx, req)
	}

	if !h.Vdb.IsHTTPSConfigEnabled() ||
		h.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) {
		return ctrl.Result{}, nil
	}

	// no-op if neither https secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start https tls config update")
	if !h.Vdb.IsTLSConfigUpdateInProgress() {
		cond := vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionTrue, "InProgress")
		if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
			h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSConfigUpdateInProgress)
			return ctrl.Result{}, err2
		}
	}

	res, err := h.Manager.setPollingCertMetadata(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	upPods := h.PFacts.FindUpPods("")
	if len(upPods) == 0 {
		h.Log.Info("No up pod found to update tls config. Restarting.")
		restartReconciler := MakeRestartReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.PFacts, true, h.Dispatcher)
		res, err2 := restartReconciler.Reconcile(ctx, req)
		return res, err2
	}

	upHostToSandbox := make(map[string]string)
	initiator := upPods[0].GetPodIP()
	for _, p := range upPods {
		upHostToSandbox[p.GetPodIP()] = p.GetSandbox()
	}
	err = h.Manager.updateTLSConfig(ctx, initiator, upHostToSandbox)
	if err != nil || h.Vdb.IsTLSCertRollbackNeeded() {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, h.handleConditions(ctx)
}

func (h *HTTPSTLSUpdateReconciler) handleConditions(ctx context.Context) error {
	var cond *metav1.Condition
	// Clear TLSConfigUpdateInProgress condition if:
	// 1) Only tls mode changed and
	// 2) ClientServer secret/mode is not changed
	// This way, we will skip nma cert rotation
	if h.Manager.TLSUpdateType == tlsModeChangeOnly && h.Vdb.NoClientServerRotationNeeded() {
		cond = vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionFalse, "Completed")
		return vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond)
	}

	cond = vapi.MakeCondition(vapi.HTTPSTLSConfigUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.HTTPSTLSConfigUpdateFinished+" to true")
		return err
	}

	return nil
}
