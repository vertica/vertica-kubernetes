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
	"github.com/google/uuid"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnsandboxSubclusterReconciler will update sandbox config maps for letting
// sandbox controller to unsandbox the subclusters
type UnsandboxSubclusterReconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	Vdb  *vapi.VerticaDB
	client.Client
}

// MakeUnsandboxSubclusterReconciler will build a UnsandboxSubclusterReconciler object
func MakeUnsandboxSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	cli client.Client) controllers.ReconcileActor {
	return &UnsandboxSubclusterReconciler{
		VRec:   vdbrecon,
		Log:    log.WithName("UnsandboxSubclusterReconciler"),
		Vdb:    vdb,
		Client: cli,
	}
}

// Reconcile will update sandbox config maps for triggering sandbox controller
func (r *UnsandboxSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db
	if r.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly || !r.Vdb.IsEON() {
		return ctrl.Result{}, nil
	}

	// update sandbox config maps for sandboxes that need to be unsandboxed
	if err := r.updateSandboxConfigMaps(ctx); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// updateSandboxConfigMaps will add a trigger ID to sandbox config maps for triggering sandbox controller
func (r *UnsandboxSubclusterReconciler) updateSandboxConfigMaps(ctx context.Context) error {
	unsandboxSbScMap := r.Vdb.GenSandboxSubclusterMapForUnsandbox()
	for sb := range unsandboxSbScMap {
		triggerUUID := uuid.NewString()
		sbMan := MakeSandboxConfigMapManager(r.VRec, r.Vdb, sb, triggerUUID)
		triggered, err := sbMan.triggerSandboxController(ctx, Unsandbox)
		if triggered {
			r.Log.Info("Sandbox ConfigMap updated. The sandbox controller will drive the unsandbox",
				"trigger-uuid", triggerUUID, "Sandbox", sb)
		} else {
			r.Log.Error(err, "failed to update sandbox config map", "sandbox", sb)
			return err
		}
	}

	return nil
}
