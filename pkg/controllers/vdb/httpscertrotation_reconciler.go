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

// HTTPSCertRotationReconciler will compare the nma tls secret with the one saved in
// "vertica.com/nma-https-previous-secret". If different, it will try to rotate the
// cert currently used with the one saved the nma tls secret for https service

const (
	noTLSChange = iota
	tlsModeChangeOnly
	httpsCertChangeOnly
	tlsModeAndCertChange
)

type HTTPSCertRotationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
	Manager    *TLSConfigManager
}

func MakeHTTPSCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &HTTPSCertRotationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("HTTPSCertRotationReconciler"),
		Dispatcher: dispatcher,
		PFacts:     pfacts,
		Manager:    MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigHTTPS, dispatcher),
	}
}

// Reconcile will rotate TLS certificate.
func (h *HTTPSCertRotationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}

	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()

	if h.Vdb.GetHTTPSTLSSecretNameInUse() == "" {
		return h.Manager.setTLSConfig(ctx, h.PFacts)
	}

	if h.Vdb.IsStatusConditionTrue(vapi.HTTPSTLSConfigUpdateFinished) && h.Vdb.IsStatusConditionTrue(vapi.TLSConfigUpdateInProgress) {
		return ctrl.Result{}, nil
	}

	// no-op if neither https secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start https cert rotation")
	cond := vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionTrue, "InProgress")
	if err2 := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err2 != nil {
		h.Log.Error(err2, "Failed to set condition to true", "conditionType", vapi.TLSConfigUpdateInProgress)
		return ctrl.Result{}, err2
	}

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

	// Clear TLSConfigUpdateInProgress condition if only tls mode changed.
	// This way, we will skip nma cert rotation
	if h.Manager.TLSUpdateType == tlsModeChangeOnly {
		cond = vapi.MakeCondition(vapi.TLSConfigUpdateInProgress, metav1.ConditionFalse, "Completed")
		return ctrl.Result{}, vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond)
	}

	cond = vapi.MakeCondition(vapi.HTTPSTLSConfigUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.HTTPSTLSConfigUpdateFinished+" to true")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
