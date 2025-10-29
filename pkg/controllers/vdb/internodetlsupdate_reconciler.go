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
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InterNodeTLSUpdateReconciler will compare the inter node tls secret or tls mode with the one saved in
// status. If different, it will try to rotate the
// cert/mode currently used with the one(s) saved the spec inter node tls.
type InterNodeTLSUpdateReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
	Manager    *TLSConfigManager
}

func MakeInterNodeTLSUpdateReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher,
	pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &InterNodeTLSUpdateReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("InterNodeTLSUpdateReconciler"),
		Dispatcher: dispatcher,
		PFacts:     pfacts,
		Manager:    MakeTLSConfigManager(vdbrecon, log, vdb, tlsConfigInterNode, dispatcher, pfacts),
	}
}

// Reconcile will rotate TLS certificate or mode.
func (h *InterNodeTLSUpdateReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if h.Vdb.IsTLSCertRollbackNeeded() && h.Vdb.IsTLSCertRollbackEnabled() && h.Vdb.GetTLSCertRollbackReason() ==
		vapi.RollbackAfterInterNodeCertRotationReason {
		return h.rollback(ctx)
	}
	if h.shouldSkipReconciler() {
		return ctrl.Result{}, nil
	}
	if err := h.updateTLSConfigEnabledInVdb(ctx); err != nil {
		return ctrl.Result{}, err
	}
	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	// initialize the manager
	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()
	if h.Vdb.GetInterNodeTLSSecretInUse() == "" {
		rec := MakeTLSConfigReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.Dispatcher, h.PFacts, vapi.InterNodeTLSConfigName, h.Manager)
		res, err2 := rec.Reconcile(ctx, req)
		return res, err2
	}
	if !h.Vdb.IsInterNodeConfigEnabled() {
		return ctrl.Result{}, nil
	}
	// no-op if neither inter node secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}
	upPods := h.PFacts.FindUpPods("")
	if len(upPods) == 0 {
		h.Log.Info("No up pod found to update internode tls config. Restarting.")
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

	cond := vapi.MakeCondition(vapi.InterNodeTLSConfigUpdateFinished, metav1.ConditionTrue, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.InterNodeTLSConfigUpdateFinished+" to true")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (h *InterNodeTLSUpdateReconciler) shouldSkipReconciler() bool {
	return !h.Vdb.IsDBInitialized() || h.Vdb.IsTLSCertRollbackNeeded() || !h.Vdb.IsInterNodeTLSAuthEnabledWithMinVersion()
}

func (h *InterNodeTLSUpdateReconciler) rollback(ctx context.Context) (ctrl.Result, error) {
	if !h.Vdb.IsTLSCertRollbackInProgress() {
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackStarted,
			"Starting %s TLS cert rollback after failed update", tlsConfigInterNode)
		// Set TLSCertRollbackInProgress and rollback
		cond := vapi.MakeCondition(vapi.TLSCertRollbackInProgress, metav1.ConditionTrue, "InProgress")
		err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond)
		if err != nil {
			return ctrl.Result{}, err
		}
		cond = &metav1.Condition{Type: vapi.TLSConfigUpdateInProgress, Status: metav1.ConditionFalse, Reason: "Completed"}

		h.Log.Info("Clearing condition", "type", cond.Type)
		if err = vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
			h.Log.Error(err, "Failed to clear condition", "type", cond.Type)
			return ctrl.Result{}, err
		}
		nm := h.Vdb.ExtractNamespacedName()
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			// Always fetch the latest in case we are in the retry loop
			if err2 := h.VRec.Client.Get(ctx, nm, h.Vdb); err2 != nil {
				return err2
			}
			spec := &vapi.TLSConfigSpec{}
			spec.Secret = h.Vdb.GetInterNodeTLSSecretInUse()
			spec.Mode = h.Vdb.GetInterNodeTLSModeInUse()
			h.Vdb.Spec.InterNodeTLS = spec
			return h.VRec.Client.Update(ctx, h.Vdb)
		})
		if err != nil {
			return ctrl.Result{}, err
		}
		conds := []metav1.Condition{
			{Type: vapi.TLSCertRollbackInProgress, Status: metav1.ConditionFalse, Reason: "Completed"},
			{Type: vapi.TLSCertRollbackNeeded, Status: metav1.ConditionFalse, Reason: "Completed"},
		}
		for _, cond := range conds {
			h.Log.Info("Clearing condition", "type", cond.Type)
			if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, &cond); err != nil {
				h.Log.Error(err, "Failed to clear condition", "type", cond.Type)
				return ctrl.Result{}, err
			}
		}
		h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackSucceeded,
			"%s TLS cert rollback completed successfully", tlsConfigInterNode)
	}
	return ctrl.Result{}, nil
}

// updateTLSConfigEnabledInVdb will set the TLS Enabled fields in the vdb spec if they
// are nil. This is to handle the case where a user created a vdb with webhook
// disabled and enabled field nil. In case the turn on the webhook later, we
// do not want it to alter the enabled field.
func (h *InterNodeTLSUpdateReconciler) updateTLSConfigEnabledInVdb(ctx context.Context) error {
	if !(h.Vdb.Spec.InterNodeTLS != nil && h.Vdb.Spec.InterNodeTLS.Enabled == nil) {
		return nil
	}
	nm := h.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := h.VRec.Client.Get(ctx, nm, h.Vdb); err != nil {
			return err
		}
		if h.Vdb.Spec.InterNodeTLS != nil && h.Vdb.Spec.InterNodeTLS.Enabled == nil {
			enabled := true
			h.Vdb.Spec.InterNodeTLS.Enabled = &enabled
		}
		return h.VRec.Client.Update(ctx, h.Vdb)
	})
}
