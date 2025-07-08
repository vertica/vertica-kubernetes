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
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

type RollbackAfterCertRotationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
}

func MakeRollbackAfterCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &RollbackAfterCertRotationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("RollbackAfterCertRotationReconciler"),
		Dispatcher: dispatcher,
		PFacts:     pfacts,
	}
}

func (r *RollbackAfterCertRotationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// If rollback is not necessary, skip
	if !r.Vdb.IsTLSCertRollbackNeeded() || r.Vdb.IsTLSCertRollbackDisabled() {
		return ctrl.Result{}, nil
	}

	// If user has not triggered rollback
	if !r.Vdb.IsTLSCertRollbackInProgress() {
		// If secret has not been reverted, alert user and exit
		if r.rollbackRequired() {
			return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
		}

		// If secret has been reverted, set TLSCertRollbackInProgress and rollback
		cond := vapi.MakeCondition(vapi.TLSCertRollbackInProgress, metav1.ConditionTrue, "InProgress")
		err := vdbstatus.UpdateCondition(ctx, r.VRec.GetClient(), r.Vdb, cond)

		if err != nil {
			return ctrl.Result{}, err
		}
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackStarted,
		"Starting TLS cert rollback after failed update")

	funcs := []func(context.Context) (ctrl.Result, error){
		r.runNMACertConfigMapReconciler,
		r.shutdownNMA,
		r.waitForNMAUp,
		r.pollNMACertHealth,
		r.runHTTPSCertRotation,
		r.cleanUpConditions,
	}

	for _, fn := range funcs {
		if res, err := fn(ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackSucceeded,
		"TLS cert rollback completed successfully")

	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) runNMACertConfigMapReconciler(ctx context.Context) (ctrl.Result, error) {
	if !r.Vdb.IsRollbackAfterNMACertRotation() {
		return ctrl.Result{}, nil
	}
	rec := MakeNMACertConfigMapReconciler(r.VRec, r.Log, r.Vdb)
	traceActorReconcile(rec, r.Log, "tls cert rollback")
	return rec.Reconcile(ctx, &ctrl.Request{})
}

func (r *RollbackAfterCertRotationReconciler) shutdownNMA(ctx context.Context) (ctrl.Result, error) {
	if !r.Vdb.IsRollbackAfterNMACertRotation() {
		return ctrl.Result{}, nil
	}

	// TODO: restart all nma containers so they can read the old cert.
	// We want to do it once, so we need to add something (e.g: a status condition)
	// that will set once we restart nma
	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) waitForNMAUp(ctx context.Context) (ctrl.Result, error) {
	if !r.Vdb.IsRollbackAfterNMACertRotation() {
		return ctrl.Result{}, nil
	}

	// TODO: find all pods and wait for each pod's nma container to be ready
	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) pollNMACertHealth(ctx context.Context) (ctrl.Result, error) {
	if !r.Vdb.IsRollbackAfterNMACertRotation() {
		return ctrl.Result{}, nil
	}

	// TODO: call rotate_nma_certs vclusterops API. We only want to poll the cert health
	// so we will skip kill NMA
	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) runHTTPSCertRotation(ctx context.Context) (ctrl.Result, error) {
	if r.Vdb.IsHTTPSRollbackFailureAfterCertHealthPolling() {
		r.Log.Info("Reverting to previous HTTPS secret")
		rec := MakeHTTPSTLSUpdateReconciler(r.VRec, r.Log, r.Vdb, r.Dispatcher, r.PFacts)
		traceActorReconcile(rec, r.Log, "tls cert rollback")
		return rec.Reconcile(ctx, &ctrl.Request{})
	}

	return ctrl.Result{}, nil
}

// cleanUpConditions clears all TLS-related status conditions that signal rotation or rollback.
// This includes:
// - TLSConfigUpdateInProgress: cert rotation has completed
// - TLSCertRollbackNeeded: rollback is no longer required
// - TLSCertRollbackInProgress: rollback has completed
func (r *RollbackAfterCertRotationReconciler) cleanUpConditions(ctx context.Context) (ctrl.Result, error) {
	conds := []metav1.Condition{
		{Type: vapi.TLSConfigUpdateInProgress, Status: metav1.ConditionFalse, Reason: "Completed"},
		{Type: vapi.TLSCertRollbackInProgress, Status: metav1.ConditionFalse, Reason: "Completed"},
		{Type: vapi.TLSCertRollbackNeeded, Status: metav1.ConditionFalse, Reason: "Completed"},
	}

	for _, cond := range conds {
		r.Log.Info("Clearing condition", "type", cond.Type)
		if err := vdbstatus.UpdateCondition(ctx, r.VRec.GetClient(), r.Vdb, &cond); err != nil {
			r.Log.Error(err, "Failed to clear condition", "type", cond.Type)
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) rollbackRequired() bool {
	currentSecret := r.Vdb.GetHTTPSNMATLSSecret()
	oldSecret := r.Vdb.GetHTTPSNMATLSSecretInUse()
	configName := vapi.HTTPSNMATLSConfigName
	if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterServerCertRotationReason {
		currentSecret = r.Vdb.GetClientServerTLSSecret()
		oldSecret = r.Vdb.GetClientServerTLSSecretInUse()
		configName = vapi.ClientServerTLSConfigName
	}

	// Check secret has been reverted
	if currentSecret != oldSecret {
		r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackNeeded,
			"TLS Rollback is required; please set %sTLS.secret to %s", configName, oldSecret)
		return true
	}
	return false
}
