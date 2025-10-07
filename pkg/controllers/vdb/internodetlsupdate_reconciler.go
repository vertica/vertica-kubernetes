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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InterNodeTLSUpdateReconciler will compare the inter node tls secret or tls mode with the one saved in
// status. If different, it will try to rotate the
// cert currently used with the one saved the inter node tls secret, and/or will update tls mode
type InterNodeTLSUpdateReconciler struct {
	VRec         *VerticaDBReconciler
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log          logr.Logger
	Dispatcher   vadmin.Dispatcher
	PFacts       *podfacts.PodFacts
	Manager      *TLSConfigManager
	FromRollback bool // Whether or not this has been called from the rollback reconciler
}

func MakeInterNodeTLSUpdateReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts, fromRollback bool) controllers.ReconcileActor {
	return &InterNodeTLSUpdateReconciler{
		VRec:         vdbrecon,
		Vdb:          vdb,
		Log:          log.WithName("InterNodeTLSUpdateReconciler"),
		Dispatcher:   dispatcher,
		PFacts:       pfacts,
		Manager:      MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigInterNode, dispatcher, pfacts),
		FromRollback: fromRollback,
	}
}

// Reconcile will rotate TLS certificate.
func (h *InterNodeTLSUpdateReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	h.Log.Info("libo: inter 1")
	// Skip if TLS not enabled, DB not initialized, or rotate has failed.
	// However, if called from rollback reconciler, always run.
	h.Log.Info("libo: is internode tls enabled " + strconv.FormatBool(h.Vdb.IsInterNodeTLSAuthEnabledWithMinVersion()))
	if h.Vdb.ShouldSkipInterNodeTLSUpdateReconcile(h.FromRollback) {
		h.Log.Info("libo: inter 2")
		return ctrl.Result{}, nil
	}
	h.Log.Info("libo: inter 3")
	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info("libo: inter 4")
	// initialize the manager
	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()

	if h.Vdb.GetInterNodeTLSSecretInUse() == "" {
		h.Log.Info("libo: inter 5")
		rec := MakeTLSConfigReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.Dispatcher, h.PFacts, vapi.InterNodeTLSConfigName, h.Manager)
		h.Log.Info("libo: inter 6")
		return rec.Reconcile(ctx, req)
	}
	h.Log.Info("libo: inter 7")
	if !h.Vdb.IsInterNodeConfigEnabled() {
		return ctrl.Result{}, nil
	}
	h.Log.Info("libo: inter 8")
	// no-op if neither inter node secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start inter node tls config update")
	cond := vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSConfigUpdateInProgress)
		return ctrl.Result{}, err2
	}
	h.Log.Info("libo: inter 9")
	if h.Vdb.IsHTTPSNMATLSAuthEnabled() {
		res, err1 := h.Manager.setPollingCertMetadata(ctx)
		if verrors.IsReconcileAborted(res, err1) {
			return res, err1
		}
	}

	upPods := h.PFacts.FindUpPods("")
	if len(upPods) == 0 {
		h.Log.Info("No up pod found to update tls config. Restarting.")
		restartReconciler := MakeRestartReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.PFacts, true, h.Dispatcher)
		res, err1 := restartReconciler.Reconcile(ctx, req)
		return res, err1
	}

	upHostToSandbox := make(map[string]string)
	initiator := upPods[0].GetPodIP()
	for _, p := range upPods {
		upHostToSandbox[p.GetPodIP()] = p.GetSandbox()
	}

	res, err := h.Manager.updateTLSConfig(ctx, initiator, upHostToSandbox)
	if verrors.IsReconcileAborted(res, err) || h.Vdb.IsTLSCertRollbackNeeded() {
		return res, err
	}

	cond = vapi.MakeCondition(vapi.InterNodeTLSConfigUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.InterNodeTLSConfigUpdateFinished+" to true")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
