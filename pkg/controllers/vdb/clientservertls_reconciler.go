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

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ClientServerTLSReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for https service

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
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}

	if h.Vdb.GetClientServerTLSModeInUse() == "" {
		return ctrl.Result{}, h.Manager.setTLSConfig()
	}

	if h.Vdb.IsStatusConditionTrue(vapi.ClientServerTLSUpdateFinished) {
		return ctrl.Result{}, nil
	}

	h.Manager.setTLSUpdateType()
	// no-op if neither https secret nor tls mode
	// changed
	if h.Manager.TLSUpdateType == noTLSChange {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start client server tls config update")

	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	res, err := h.Manager.setHTTPSTLSUpdateData(ctx)
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

	// temporary just for testing
	sec := vapi.MakeClientServerTLSSecretRef(h.Vdb.Spec.ClientServerTLSSecret)
	return ctrl.Result{}, vdbstatus.UpdateSecretRef(ctx, h.VRec.GetClient(), h.Vdb, sec)
}
