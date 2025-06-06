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

// ClientServerTLSReconciler will compare the client server tls secret or tls mode with the one saved in
// status. If different, it will try to rotate the
// cert currently used with the one saved the client server tls secret, and/or will update tls mode
type ClientServerTLSReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
	Manager    *TLSConfigManager
}

func MakeClientServerTLSReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &ClientServerTLSReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("ClientServerTLSReconciler"),
		Dispatcher: dispatcher,
		PFacts:     pfacts,
		Manager:    MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigServer, dispatcher),
	}
}

// Reconcile will rotate TLS certificate.
func (h *ClientServerTLSReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsSetForTLS() && !h.Vdb.IsTLSConfigEnabled() {
		return ctrl.Result{}, nil
	}

	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()

	if h.Vdb.GetClientServerTLSSecretNameInUse() == "" {
		return h.Manager.setTLSConfig(ctx, h.PFacts)
	}

	if h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSUpdateFinished) &&
		h.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) {
		return ctrl.Result{}, nil
	}

	// no-op if neither client server secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start client server cert rotation")
	cond := vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSConfigUpdateInProgress)
		return ctrl.Result{}, err2
	}

	h.Log.Info("start client server tls config update")

	res, err := h.Manager.setPollingCertMetadata(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	initiatorPod, ok := h.PFacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No up pod found to update tls config. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	err = h.Manager.updateTLSConfig(ctx, initiatorPod.GetPodIP())
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := h.Manager.updateTLSModeInStatus(ctx); err != nil {
		return ctrl.Result{}, err
	}

	cond = vapi.MakeCondition(vapi.ClientServerTLSUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.ClientServerTLSUpdateFinished+" to true")
		return ctrl.Result{}, err
	}

	// temporary just for testing
	sec := vapi.MakeClientServerTLSSecretRef(h.Vdb.Spec.ClientServerTLSSecret)
	return ctrl.Result{}, vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, sec)
}
