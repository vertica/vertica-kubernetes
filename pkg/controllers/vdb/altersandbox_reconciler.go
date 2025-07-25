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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
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
	if len(a.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}

	for i := range a.Vdb.Spec.Sandboxes {
		sb := &a.Vdb.Spec.Sandboxes[i]
		if a.Vdb.GetSandboxStatus(sb.Name) == nil {
			a.Log.Info("skip reconcile Alter Sandbox as sandbox does not exist in the database yet", "sandbox", sb.Name)
			continue
		}
		err := a.reconcileAlterSandbox(ctx, sb.Name)
		if verrors.IsReconcileAborted(ctrl.Result{}, err) {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// reconcileAlterSandbox will handle sandbox configmap update based on the sandbox image change
func (a *AlterSandboxTypeReconciler) reconcileAlterSandbox(ctx context.Context, sbName string) error {
	triggerUUID := uuid.NewString()
	sbMan := MakeSandboxConfigMapManager(a.VRec, a.Vdb, sbName, triggerUUID)
	err := sbMan.fetchConfigMap(ctx)
	if err != nil {
		return fmt.Errorf("failed to fetch sandbox configmap for %s: %w", sbName, err)
	}
	// skip reconcile Alter Sandbox if alter sandbox trigger id is already set
	if sbMan.configMap.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID] != "" {
		return nil
	}

	if ok, needErr := a.isAlterSandboxNeeded(ctx, sbName); !ok || needErr != nil {
		return needErr
	}
	// Once we find out that a sandbox upgrade is needed, we need to wake up
	// the sandbox controller to drive it. We will use a SandboxConfigMapManager object
	// that will update that sandbox's configmap watched by the sandbox controller
	triggered, err := sbMan.triggerSandboxController(ctx, AlterSubclusterType)
	if triggered {
		a.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the alter sandbox subcluster type",
			"trigger-uuid", triggerUUID, "Sandbox", sbName)
	}
	return err
}

// isAlterSandboxNeeded checks whether an alter sandbox is needed
func (a *AlterSandboxTypeReconciler) isAlterSandboxNeeded(ctx context.Context, sbName string) (bool, error) {
	// We need to copy the pod facts for the sandbox as this reconciler is called by verticadb_controller only
	// While, the PFacts.Copy function cleared all the details and it could not detect any running pods in the unit test.
	// A PFacts with sandboxName set is expected in the unit test.
	sbpfacts := a.PFacts.Copy(sbName)
	if a.PFacts.SandboxName == sbName {
		sbpfacts = *a.PFacts
	}

	if err := sbpfacts.Collect(ctx, a.Vdb); err != nil {
		return false, fmt.Errorf("failed to collect pod facts for sandbox %s: %w", sbName, err)
	}
	sb := a.Vdb.GetSandbox(sbName)
	if sb == nil {
		return false, fmt.Errorf("could not find sandbox %s", sbName)
	}
	for _, sc := range sb.Subclusters {
		pf, ok := sbpfacts.FindFirstUpPod(true, sc.Name)
		if !ok {
			// We need go through all sandboxes subclusters to determine if an alter is needed.
			// So we can skip if some of the pods may not be up yet, or some of the sandboxes are not running
			continue
		}
		// Need alter only when sandbox subcluster type don't match podfacts (which reads the database)
		if sc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			a.Log.Info("Alter sandbox needed", "subcluster", sc.Name, "type",
				sc.Type, "podfacts is primary", pf.GetIsPrimary())
			return true, nil
		}
	}
	return false, nil
}
