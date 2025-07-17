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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"

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

	if !r.Vdb.IsTLSCertRollbackInProgress() {
		// Set TLSCertRollbackInProgress and rollback
		cond := vapi.MakeCondition(vapi.TLSCertRollbackInProgress, metav1.ConditionTrue, "InProgress")
		err := vdbstatus.UpdateCondition(ctx, r.VRec.GetClient(), r.Vdb, cond)

		if err != nil {
			return ctrl.Result{}, err
		}
	}

	tlsConfigName := tlsConfigHTTPS
	if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterServerCertRotationReason {
		tlsConfigName = tlsConfigServer
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackStarted,
		"Starting %s TLS cert rollback after failed update", tlsConfigName)

	funcs := []func(context.Context) (ctrl.Result, error){
		r.runNMACertConfigMapReconciler,
		r.shutdownNMA,
		r.waitForNMAUp,
		r.pollNMACertHealth,
		r.runHTTPSCertRotation,
		r.resetTLSUpdateCondition,
		r.updateTLSConfigInVdb,
		r.cleanUpRollbackConditions,
	}

	for _, fn := range funcs {
		if res, err := fn(ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.TLSCertRollbackSucceeded,
		"%s TLS cert rollback completed successfully", tlsConfigName)

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
		// TODO: Run HTTPS TLS rollback
	}

	return ctrl.Result{}, nil
}

// cleanUpRollbackConditions clears all TLS-related status conditions that signal rollback.
// This includes:
// - TLSCertRollbackNeeded: rollback is no longer required
// - TLSCertRollbackInProgress: rollback has completed
func (r *RollbackAfterCertRotationReconciler) cleanUpRollbackConditions(ctx context.Context) (ctrl.Result, error) {
	conds := []metav1.Condition{
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

// resetTLSUpdateCondition will reset the TLSConfigUpdateInProgress condition
// This needs to be done before updating the VDB, since VDB cannot be changed
// when TLSConfigUpdateInProgress is true
func (r *RollbackAfterCertRotationReconciler) resetTLSUpdateCondition(ctx context.Context) (ctrl.Result, error) {
	cond := metav1.Condition{Type: vapi.TLSConfigUpdateInProgress, Status: metav1.ConditionFalse, Reason: "Completed"}

	r.Log.Info("Clearing condition", "type", cond.Type)
	if err := vdbstatus.UpdateCondition(ctx, r.VRec.GetClient(), r.Vdb, &cond); err != nil {
		r.Log.Error(err, "Failed to clear condition", "type", cond.Type)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateTLSConfigInVdb will revert the secret/mode in the spec to the previous good values,
// which are retrieved from the status
func (r *RollbackAfterCertRotationReconciler) updateTLSConfigInVdb(ctx context.Context) (ctrl.Result, error) {
	r.Log.Info("Reverting TLS Config in VDB spec")
	nm := r.Vdb.ExtractNamespacedName()
	return ctrl.Result{}, retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest in case we are in the retry loop
		if err := r.VRec.Client.Get(ctx, nm, r.Vdb); err != nil {
			return err
		}
		if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterServerCertRotationReason {
			r.Vdb.Spec.ClientServerTLS = &vapi.TLSConfigSpec{
				Secret: r.Vdb.GetClientServerTLSSecretInUse(),
				Mode:   r.Vdb.GetClientServerTLSModeInUse(),
			}
		} else {
			r.Vdb.Spec.HTTPSNMATLS = &vapi.TLSConfigSpec{
				Secret: r.Vdb.GetHTTPSNMATLSSecretInUse(),
				Mode:   r.Vdb.GetHTTPSTLSModeInUse(),
			}
		}
		return r.VRec.Client.Update(ctx, r.Vdb)
	})
}
