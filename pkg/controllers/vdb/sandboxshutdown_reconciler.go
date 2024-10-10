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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SandboxShutdownReconciler
type SandboxShutdownReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on
	Manager UpgradeManager
}

// MakeSandboxShutdownReconciler will build a SandboxShutdownReconciler object
func MakeSandboxShutdownReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &SandboxShutdownReconciler{
		VRec: vdbrecon,
		Log:  log.WithName("SandboxShutdownReconciler"),
		Vdb:  vdb,
	}
}

func (s *SandboxShutdownReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op as there is no sandbox
	if len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}
	for i := range s.Vdb.Spec.Sandboxes {
		sb := &s.Vdb.Spec.Sandboxes[i]
		res, err := s.reconcileSandboxShutdown(ctx, sb)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileSandboxShutdown updates the sandbox configmap in order to
// trigger shutdown or restart of the sandbox.
func (s *SandboxShutdownReconciler) reconcileSandboxShutdown(ctx context.Context, sb *vapi.Sandbox) (ctrl.Result, error) {
	sbName := sb.Name
	sbStatus := s.Vdb.GetSandboxStatus(sbName)
	if sbStatus == nil {
		s.Log.Info("Requeue because the sandbox does not exist yet", "sandbox", sbName)
		return ctrl.Result{Requeue: true}, nil
	}
	if sb.Shutdown == sbStatus.Shutdown {
		return ctrl.Result{}, nil
	}
	op := "shutdown"
	if sbStatus.Shutdown {
		op = "restart"
	}
	triggerUUID := uuid.NewString()
	sbMan := MakeSandboxConfigMapManager(s.VRec, s.Vdb, sbName, triggerUUID)
	triggered, err := sbMan.triggerSandboxController(ctx, Shutdown)
	if triggered {
		s.Log.Info(fmt.Sprintf("Sandbox ConfigMap updated. The sandbox controller will drive the %s", op),
			"trigger-uuid", triggerUUID, "Sandbox", sbName)
	}
	return ctrl.Result{}, err
}
