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
	if !r.Vdb.IsTLSCertRollbackNeeded() || r.Vdb.IsTLSCertRollbackDisabled() {
		return ctrl.Result{}, nil
	}

	funcs := []func(context.Context) (ctrl.Result, error){
		r.runNMACertConfigMapReconciler,
		r.shutdownNMA,
		r.waitForNMAUp,
		r.pollNMACertHealth,
		r.httpsCertRotation,
		r.updateNMATLSSecretInVdb,
		r.cleanUpConditions,
	}

	for _, fn := range funcs {
		if res, err := fn(ctx); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

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

func (r *RollbackAfterCertRotationReconciler) httpsCertRotation(ctx context.Context) (ctrl.Result, error) {
	if r.Vdb.IsRollbackFailureBeforeCertHealthPolling() {
		return ctrl.Result{}, nil
	}

	// TODO: call httpscertrotation_reconciler. Changes are needed there so it can be
	// reused for rollback
	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) updateNMATLSSecretInVdb(ctx context.Context) (ctrl.Result, error) {
	// TODO: change spec.NMATLSSecret to its original value before cert rotation
	return ctrl.Result{}, nil
}

func (r *RollbackAfterCertRotationReconciler) cleanUpConditions(ctx context.Context) (ctrl.Result, error) {
	// TODO: we can clean up all conditions set for cert rotation and rollback
	return ctrl.Result{}, nil
}
