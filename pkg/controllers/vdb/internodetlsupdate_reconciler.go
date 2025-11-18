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
	if h.Vdb.ShouldSkipInterNodeTLSUpdateReconcile() {
		return ctrl.Result{}, nil
	}
	err := h.PFacts.Collect(ctx, h.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	// initialize the manager
	h.Manager.setTLSUpdatedata()
	h.Manager.setTLSUpdateType()
	// If no secret is in use yet, run initial TLS configuration
	if h.Vdb.GetInterNodeTLSSecretInUse() == "" {
		rec := MakeTLSConfigReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.Dispatcher, h.PFacts, vapi.InterNodeTLSConfigName, h.Manager)
		res, err2 := rec.Reconcile(ctx, req)
		return res, err2
	}
	// After initial configuration, check if inter-node config is enabled in DB
	if !h.Vdb.IsInterNodeConfigEnabled() {
		return ctrl.Result{}, nil
	}
	// no-op if neither inter node secret nor tls mode
	// changed
	if !h.Manager.needTLSConfigChange() {
		return ctrl.Result{}, nil
	}
	if err2 := h.updateCondition(ctx, metav1.ConditionTrue); err2 != nil {
		return ctrl.Result{}, err2
	}
	upPods := h.PFacts.FindUpPods("")
	if len(upPods) == 0 {
		h.Log.Info("No up pod found to update internode tls config. Restarting.")
		restartReconciler := MakeRestartReconciler(h.VRec, h.Log, h.Vdb, h.PFacts.PRunner, h.PFacts, true, h.Dispatcher)
		res, err1 := restartReconciler.Reconcile(ctx, req)
		return res, err1
	}

	initiator, upHostToSandbox := h.prepareHosts(upPods)

	res, err := h.Manager.updateTLSConfig(ctx, initiator, upHostToSandbox)
	if verrors.IsReconcileAborted(res, err) || h.Vdb.IsTLSCertRollbackNeeded() {
		return res, err
	}

	// Update the status with the new secret and mode after successful rotation
	if err := h.updateTLSConfigInStatus(ctx); err != nil {
		return ctrl.Result{}, err
	}

	if err := h.updateCondition(ctx, metav1.ConditionFalse); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// updateTLSConfigInStatus updates the InterNode TLS config in status after successful rotation
func (h *InterNodeTLSUpdateReconciler) updateTLSConfigInStatus(ctx context.Context) error {
	tls := vapi.MakeInterNodeTLSConfig(h.Vdb.GetInterNodeTLSSecret(), h.Vdb.GetInterNodeTLSMode())
	if err := vdbstatus.UpdateTLSConfigs(ctx, h.VRec.GetClient(), h.Vdb, []*vapi.TLSConfigStatus{tls}); err != nil {
		h.Log.Error(err, "failed to update InterNode TLS config in status")
		return err
	}
	h.Log.Info("saved new InterNode TLS cert secret name and mode in status",
		"secret", h.Vdb.GetInterNodeTLSSecret(), "mode", h.Vdb.GetInterNodeTLSMode())
	return nil
}

func (h *InterNodeTLSUpdateReconciler) updateCondition(ctx context.Context, trueOrFalse metav1.ConditionStatus) error {
	cond := vapi.MakeCondition(vapi.InterNodeTLSConfigUpdateInProgress, trueOrFalse, "Completed")
	if err := vdbstatus.UpdateCondition(ctx, h.VRec.GetClient(), h.Vdb, cond); err != nil {
		h.Log.Error(err, "failed to set condition "+vapi.InterNodeTLSConfigUpdateInProgress)
		return err
	}
	return nil
}

func (h *InterNodeTLSUpdateReconciler) prepareHosts(upPods []*podfacts.PodFact) (initiator string, upHostToSandbox map[string]string) {
	upHostToSandbox = make(map[string]string)
	initiator = upPods[0].GetPodIP()
	for _, p := range upPods {
		upHostToSandbox[p.GetPodIP()] = p.GetSandbox()
	}
	return initiator, upHostToSandbox
}
