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
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSReconciler will turn on the tls config when users request it
type TLSReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	if err := h.updateTLSConfigEnabledInVdb(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if !h.Vdb.IsAnyTLSAuthEnabledWithMinVersion() {
		return ctrl.Result{}, nil
	}
	actors := []controllers.ReconcileActor{}
	// when we first set tls config and nma tls secret is different than https tls secret,
	// we need to restart nma
	if h.Vdb.IsDBInitialized() &&
		h.Vdb.GetHTTPSNMATLSSecretInUse() == "" &&
		h.Vdb.GetClientServerTLSSecretInUse() == "" &&
		h.Vdb.Spec.NMATLSSecret != "" &&
		(h.Vdb.Spec.NMATLSSecret != h.Vdb.GetHTTPSNMATLSSecret() || h.Vdb.Spec.NMATLSSecret != h.Vdb.GetClientServerTLSSecret()) {
		h.Log.Info("Adding nma cert rotation reconciler to restart nma for new tls secret")
		actors = append(actors, MakeNMACertRotationReconciler(h.VRec, h.Log, h.Vdb, h.Dispatcher, h.Pfacts, true))
	}
	actors = append(actors, h.constructActors(h.Log, h.Vdb, h.Pfacts, h.Dispatcher)...)
	for _, actor := range actors {
		res, err := actor.Reconcile(ctx, request)
		if verrors.IsReconcileAborted(res, err) {
			h.Log.Error(err, "failed to reconcile tls configuration")
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

func (h *TLSReconciler) constructActors(log logr.Logger, vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts,
	dispatcher vadmin.Dispatcher) []controllers.ReconcileActor {
	return []controllers.ReconcileActor{
		// update https tls by setting the tls config, rotating the cert and/or changing tls mode
		MakeHTTPSTLSUpdateReconciler(h.VRec, log, vdb, dispatcher, pfacts, false),
		// Update NMA config map for values related to HTTPS
		MakeNMACertConfigMapReconciler(h.VRec, log, vdb),
		// rotate nma tls cert only if httpsNMA secret name is changed in vdb.spec
		MakeNMACertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts, false),
		// update client server tls by setting the tls config, rotating the cert and/or changing tls mode
		MakeClientServerTLSUpdateReconciler(h.VRec, log, vdb, dispatcher, pfacts, false),
		// Update NMA config map for values related to ClientServer
		MakeNMACertConfigMapReconciler(h.VRec, log, vdb),
		// rotate nma tls cert only if clientServer secret name is changed in vdb.spec
		MakeNMACertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts, false),
		// update inter node tls by setting the tls config, rotating the cert and/or changing tls mode
		MakeInterNodeTLSUpdateReconciler(h.VRec, log, vdb, dispatcher, pfacts),
		// rollback, in case of failure, any cert rotation op related to https or client-server or inter-node TLS
		MakeRollbackAfterCertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts),
	}
}

// updateTLSConfigEnabledInVdb will set the TLS Enabled fields in the vdb spec if they
// are nil. This is to handle the case where a user created a vdb with webhook
// disabled and enabled field nil. In case the turn on the webhook later, we
// do not want it to alter the enabled field.
func (h *TLSReconciler) updateTLSConfigEnabledInVdb(ctx context.Context) error {
	if !h.Vdb.ShouldSetTLSEnabled() {
		return nil
	}
	nm := h.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := h.VRec.Client.Get(ctx, nm, h.Vdb); err != nil {
			return err
		}

		if h.Vdb.Spec.HTTPSNMATLS != nil && h.Vdb.Spec.HTTPSNMATLS.Enabled == nil {
			enabled := true
			h.Vdb.Spec.HTTPSNMATLS.Enabled = &enabled
		}
		if h.Vdb.Spec.ClientServerTLS != nil && h.Vdb.Spec.ClientServerTLS.Enabled == nil {
			enabled := true
			h.Vdb.Spec.ClientServerTLS.Enabled = &enabled
		}

		return h.VRec.Client.Update(ctx, h.Vdb)
	})
}
