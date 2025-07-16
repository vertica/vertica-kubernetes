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
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AlterSandboxTypeReconciler will change a sandbox subcluster type in db
type AlterSandboxTypeReconciler struct {
	VRec   config.ReconcilerInterface
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *podfacts.PodFacts
}

// MakeAlterSandboxTypeReconciler will build a AlterSandboxTypeReconciler object
func MakeAlterSandboxTypeReconciler(vdbrecon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &AlterSandboxTypeReconciler{
		VRec:   vdbrecon,
		Log:    log.WithName("AlterSandboxTypeReconciler"),
		Vdb:    vdb,
		PFacts: pfacts,
	}
}

func (a *AlterSandboxTypeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op as there is no sandbox
	if len(a.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}
	for i := range a.Vdb.Spec.Sandboxes {
		sb := &a.Vdb.Spec.Sandboxes[i]
		res, err := a.reconcileAlterSandbox(ctx, sb.Name)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileAlterSandbox will handle sandbox configmap update based on the sandbox image change
func (a *AlterSandboxTypeReconciler) reconcileAlterSandbox(ctx context.Context, sbName string) (ctrl.Result, error) {
	if a.Vdb.GetSandboxStatus(sbName) == nil {
		return ctrl.Result{Requeue: true}, nil
	}
	if _, ok, err := a.isAlterSandboxNeeded(sbName); !ok || err != nil {
		return ctrl.Result{}, err
	}
	// Once we find out that a sandbox upgrade is needed, we need to wake up
	// the sandbox controller to drive it. We will use a SandboxConfigMapManager object
	// that will update that sandbox's configmap watched by the sandbox controller
	triggerUUID := uuid.NewString()
	sbMan := MakeSandboxConfigMapManager(a.VRec, a.Vdb, sbName, triggerUUID)
	triggered, err := sbMan.triggerSandboxController(ctx, AlterSubclusterType)
	if triggered {
		a.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the alter sandbox subcluster type",
			"trigger-uuid", triggerUUID, "Sandbox", sbName)
	}
	return ctrl.Result{}, err
}

// isAlterSandboxNeeded checks whether an alter sandbox is needed
func (a *AlterSandboxTypeReconciler) isAlterSandboxNeeded(sbName string) (ctrl.Result, bool, error) {
	sb := a.Vdb.GetSandbox(sbName)
	if sb == nil {
		return ctrl.Result{}, false, fmt.Errorf("could not find sandbox %s", sbName)
	}
	for _, sc := range sb.Subclusters {
		pf, ok := a.PFacts.FindFirstUpPod(false, sc.Name)
		if !ok {
			a.Log.Info("Requeue is alter sandbox needed: could not find pod for sandbox subcluster", "subcluster", sc.Name)
			return ctrl.Result{Requeue: true}, false, nil
		}
		// Need alter only when sandbox subcluster type don't match podfacts (which reads the database)
		if sc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			return ctrl.Result{}, true, nil
		}
	}
	return ctrl.Result{}, false, nil
}
