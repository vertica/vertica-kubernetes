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
	"strings"

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
	if !r.Vdb.IsTLSCertRollbackNeeded() || !r.Vdb.IsTLSCertRollbackEnabled() {
		return ctrl.Result{}, nil
	}

	if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterInterNodeCertRotationReason {
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
		r.runHTTPSCertRotation,
		r.resetTLSUpdateCondition,
		r.setAutoRotateStatus,
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

// runHTTPSCertRotation will re-run HTTPS cert rotate to restore the last good secret and mode;
// this is needed when there is a failure in HTTPS rotate after DB has been updated
func (r *RollbackAfterCertRotationReconciler) runHTTPSCertRotation(ctx context.Context) (ctrl.Result, error) {
	if r.Vdb.IsHTTPSRollbackFailureAfterCertHealthPolling() {
		r.Log.Info("Reverting to previous HTTPS secret", "secretName", r.Vdb.GetHTTPSNMATLSSecretInUse())
		rec := MakeHTTPSTLSUpdateReconciler(r.VRec, r.Log, r.Vdb, r.Dispatcher, r.PFacts, true)
		traceActorReconcile(rec, r.Log, "tls cert rollback")
		return rec.Reconcile(ctx, &ctrl.Request{})
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
		mode := ""
		tlsConfigName := ""
		if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterServerCertRotationReason {
			tlsConfigName = vapi.ClientServerTLSConfigName
			mode = r.Vdb.GetClientServerTLSModeInUse()
			if strings.EqualFold(r.Vdb.GetClientServerTLSMode(), mode) {
				mode = r.Vdb.GetSpecClientServerTLSMode()
			}
			// Preserve all existing fields
			spec := r.Vdb.Spec.ClientServerTLS.DeepCopy()
			if spec == nil {
				spec = &vapi.TLSConfigSpec{}
			}
			spec.Secret = r.Vdb.GetClientServerTLSSecretInUse()
			spec.Mode = mode
			r.Vdb.Spec.ClientServerTLS = spec
		} else {
			tlsConfigName = vapi.HTTPSNMATLSConfigName
			mode = r.Vdb.GetHTTPSTLSModeInUse()
			if strings.EqualFold(r.Vdb.GetHTTPSNMATLSMode(), mode) {
				mode = r.Vdb.GetSpecHTTPSNMATLSMode()
			}
			// Preserve all existing fields
			spec := r.Vdb.Spec.HTTPSNMATLS.DeepCopy()
			if spec == nil {
				spec = &vapi.TLSConfigSpec{}
			}
			spec.Secret = r.Vdb.GetHTTPSNMATLSSecretInUse()
			spec.Mode = mode
			r.Vdb.Spec.HTTPSNMATLS = spec
		}
		r.Log.Info("Updating VDB spec during rollback", "tlsConfigName", tlsConfigName, "secret",
			r.Vdb.GetTLSConfigSpecByName(tlsConfigName).Secret)
		return r.VRec.Client.Update(ctx, r.Vdb)
	})
}

// setAutoRotateStatus will set the AutoRotateFailedSecret in status with the failing secret.
// This is used to indicate that the auto-rotation of TLS secrets has failed
// and the operator should auto-rotate to the next secret.
func (r *RollbackAfterCertRotationReconciler) setAutoRotateStatus(ctx context.Context) (ctrl.Result, error) {
	tlsConfigName := vapi.HTTPSNMATLSConfigName
	failedSecret := r.Vdb.GetHTTPSNMATLSSecret()
	if r.Vdb.GetTLSCertRollbackReason() == vapi.RollbackAfterServerCertRotationReason {
		tlsConfigName = vapi.ClientServerTLSConfigName
		failedSecret = r.Vdb.GetClientServerTLSSecret()
	}

	if len(r.Vdb.GetAutoRotateSecrets(tlsConfigName)) == 0 {
		return ctrl.Result{}, nil
	}

	r.Log.Info("Setting AutoRotateFailedSecret for TLSConfigStatus", "tlsConfigName", tlsConfigName, "failedSecret", failedSecret)

	patchStatus := r.Vdb.GetTLSConfigByName(tlsConfigName)
	patchStatus.AutoRotateFailedSecret = failedSecret

	// Patch status explicitly
	if err := vdbstatus.UpdateTLSConfigs(ctx, r.VRec.Client, r.Vdb, []*vapi.TLSConfigStatus{patchStatus}); err != nil {
		r.Log.Error(err, "Failed to patch TLSConfigStatus with AutoRotateFailed", "tlsConfigName", tlsConfigName)
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
