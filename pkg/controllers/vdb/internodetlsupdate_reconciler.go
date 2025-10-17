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
	"k8s.io/client-go/util/retry"

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
	if h.Vdb.IsTLSCertRollbackNeeded() && h.Vdb.IsTLSCertRollbackEnabled() && h.Vdb.GetTLSCertRollbackReason() ==
		vapi.RollbackAfterInterNodeCertRotationReason {
		return h.rollback(ctx)
	}
	if h.Vdb.ShouldSkipInterNodeTLSUpdateReconcile(h.FromRollback) {
		return ctrl.Result{}, nil
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
		res, err := rec.Reconcile(ctx, req)
		return res, err
	}
	if !h.Vdb.IsInterNodeConfigEnabled() {
		return ctrl.Result{}, nil
	}
	// no-op if neither inter node secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}

	h.Log.Info("start inter node tls config update")
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

func (h *InterNodeTLSUpdateReconciler) rollback(ctx context.Context) (ctrl.Result, error) {
	if !h.Vdb.IsTLSCertRollbackInProgress() {
		// Set TLSCertRollbackInProgress and rollback
		cond := vapi.MakeCondition(vapi.TLSCertRollbackInProgress, metav1.ConditionTrue, "InProgress")
		err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond)
		if err != nil {
			return ctrl.Result{}, err
		}
		cond = &metav1.Condition{Type: vapi.TLSConfigUpdateInProgress, Status: metav1.ConditionFalse, Reason: "Completed"}

		h.Log.Info("Clearing condition", "type", cond.Type)
		if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
			h.Log.Error(err, "Failed to clear condition", "type", cond.Type)
			return ctrl.Result{}, err
		}
		nm := h.Vdb.ExtractNamespacedName()
		err = retry.RetryOnConflict(retry.DefaultBackoff, func() error {
			// Always fetch the latest in case we are in the retry loop
			if err := h.VRec.Client.Get(ctx, nm, h.Vdb); err != nil {
				return err
			}
			spec := &vapi.TLSConfigSpec{}
			spec.Secret = h.Vdb.GetInterNodeTLSSecretInUse()
			spec.Mode = h.Vdb.GetInterNodeTLSModeInUse()
			h.Vdb.Spec.InterNodeTLS = spec
			return h.VRec.Client.Update(ctx, h.Vdb)
		})
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
	}
	return ctrl.Result{}, nil
}
