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
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ClientServerTLSUpdateReconciler will compare the client server tls secret or tls mode with the one saved in
// status. If different, it will try to rotate the
// cert currently used with the one saved the client server tls secret, and/or will update tls mode
type ClientServerTLSUpdateReconciler struct {
	VRec         *VerticaDBReconciler
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log          logr.Logger
	Dispatcher   vadmin.Dispatcher
	PFacts       *podfacts.PodFacts
	Manager      *TLSConfigManager
	FromRollback bool // Whether or not this has been called from the rollback reconciler
}

func MakeClientServerTLSUpdateReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts, fromRollback bool) controllers.ReconcileActor {
	return &ClientServerTLSUpdateReconciler{
		VRec:         vdbrecon,
		Vdb:          vdb,
		Log:          log.WithName("ClientServerTLSUpdateReconciler"),
		Dispatcher:   dispatcher,
		PFacts:       pfacts,
		Manager:      MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigServer, dispatcher),
		FromRollback: fromRollback,
	}
}

// Reconcile will rotate TLS certificate.
func (h *ClientServerTLSUpdateReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
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

	if h.Vdb.GetClientServerTLSSecretInUse() == "" {
		rec := MakeTLSConfigReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.Dispatcher, h.PFacts, vapi.ClientServerTLSConfigName, h.Manager)
		return rec.Reconcile(ctx, req)
	}

	if !h.Vdb.IsClientServerConfigEnabled() ||
		h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSConfigUpdateFinished) {
		return ctrl.Result{}, nil
	}

	// no-op if neither client server secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start client server tls config update")
	cond := vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSConfigUpdateInProgress)
		return ctrl.Result{}, err2
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
	if err != nil {
		return ctrl.Result{}, err
	}

	cond = vapi.MakeCondition(vapi.ClientServerTLSConfigUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.ClientServerTLSConfigUpdateFinished+" to true")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}
