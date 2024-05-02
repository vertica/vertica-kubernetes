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
	ctrl "sigs.k8s.io/controller-runtime"
)

// SandboxUpgradeReconciler will handle the process when a sandbox
// image changes
type SandboxUpgradeReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on
	Manager UpgradeManager
}

// MakeSandboxUpgradeReconciler will build a SandboxUpgradeReconciler object
func MakeSandboxUpgradeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	fn := func(vdb *vapi.VerticaDB) bool { return true }
	return &SandboxUpgradeReconciler{
		VRec:    vdbrecon,
		Log:     log.WithName("SandboxUpgradeReconciler"),
		Vdb:     vdb,
		Manager: *MakeUpgradeManager(vdbrecon, log, vdb, vapi.OfflineUpgradeInProgress, fn),
	}
}

func (s *SandboxUpgradeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op as there is no sandbox
	if len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}
	for i := range s.Vdb.Spec.Sandboxes {
		sb := &s.Vdb.Spec.Sandboxes[i]
		res, err := s.reconcileSandboxImage(ctx, sb.Name)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileSandboxImage will handle sandbox configmap update based on the sandbox image change
func (s *SandboxUpgradeReconciler) reconcileSandboxImage(ctx context.Context, sbName string) (ctrl.Result, error) {
	if !s.doesSandboxExist(sbName) {
		s.Log.Info("Requeue because the sandbox does not exist yet", "sandbox", sbName)
		return ctrl.Result{Requeue: true}, nil
	}
	if ok, err := s.isSandboxUpgradeNeeded(ctx, sbName); !ok || err != nil {
		return ctrl.Result{}, err
	}
	// Once we find out that a sandbox upgrade is needed, we need to wake up
	// the sandbox controller to drive it. We will use a SandboxTrigger object
	// that will update that sandbox's configmap watched by the sandbox controller
	sbTrigger := MakeSandboxTrigger(s.VRec, s.Vdb, sbName)
	triggered, err := sbTrigger.triggerSandboxController(ctx)
	if triggered {
		s.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the upgrade", "Sandbox", sbName)
	}
	return ctrl.Result{}, err
}

// isSandboxUpgradeNeeded checks whether an upgrade is needed and/or in progress
// for a given sandbox. It will return true for the first parm if an upgrade should
// proceed.
func (s *SandboxUpgradeReconciler) isSandboxUpgradeNeeded(ctx context.Context, sbName string) (bool, error) {
	if ok := s.Manager.isUpgradeInProgress(sbName); ok {
		// If upgrade is in progress, we return false
		// because the sandbox controller has already been
		// triggered. The caller is going to skip this sandbox
		return false, nil
	}
	return s.Manager.isVDBImageDifferent(ctx, sbName)
}

// doesSandboxExist returns true if the sandbox has already been created
func (s *SandboxUpgradeReconciler) doesSandboxExist(sbName string) bool {
	return s.Vdb.GetSandboxStatus(sbName) != nil
}
