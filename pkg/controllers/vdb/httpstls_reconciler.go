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
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSConfigReconciler will turn on the tls config when users request it
type HTTPSTLSReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeHTTPSTLSReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &HTTPSTLSReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("HTTPSTLSReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *HTTPSTLSReconciler) Reconcile(ctx context.Context, request *ctrl.Request) (ctrl.Result, error) {
	actors := h.constructActors(h.Log, h.Vdb, h.PRunner, h.Pfacts, h.Dispatcher)
	for _, actor := range actors {
		res, err := actor.Reconcile(ctx, request)
		if verrors.IsReconcileAborted(res, err) {
			h.Log.Error(err, "failed to reconcile https tls configuration")
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

func (h *HTTPSTLSReconciler) constructActors(log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts,
	dispatcher vadmin.Dispatcher) []controllers.ReconcileActor {
	return []controllers.ReconcileActor{
		// set up initial tls configuration for https service after db creation, reviving or upgrading
		MakeTLSConfigReconciler(h.VRec, log, vdb, prunner, dispatcher, pfacts, vapi.HTTPSNMATLSConfigName),
		MakeTLSConfigReconciler(h.VRec, log, vdb, prunner, dispatcher, pfacts, vapi.ClientServerTLSConfigName),
		// rotate https tls cert when tls cert secret name is changed in vdb.spec
		MakeHTTPSCertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts),
		// updates nma config map with the name of the new secret
		MakeObjReconciler(h.VRec, log, vdb, pfacts, ObjReconcileModeNMAConfigMap),
		// rotate nma tls cert when tls cert secret name is changed in vdb.spec
		MakeNMACertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts),
		// rollback, in case of failure, any cert rotation op related to nmaTLSSecret
		MakeRollbackAfterNMACertRotationReconciler(h.VRec, log, vdb, dispatcher, pfacts),
	}
}
