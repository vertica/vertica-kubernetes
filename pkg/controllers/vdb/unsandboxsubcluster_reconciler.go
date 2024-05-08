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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnsandboxSubclusterReconciler will update sandbox config maps for letting
// sandbox controller to unsandbox the subclusters
type UnsandboxSubclusterReconciler struct {
	Log logr.Logger
	Vdb *vapi.VerticaDB
	client.Client
}

// MakeUnsandboxSubclusterReconciler will build a UnsandboxSubclusterReconciler object
func MakeUnsandboxSubclusterReconciler(log logr.Logger, vdb *vapi.VerticaDB,
	cli client.Client) controllers.ReconcileActor {
	return &UnsandboxSubclusterReconciler{
		Log:    log.WithName("VdbControllerUnsandboxSubclusterReconciler"),
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

	// delete expired sandbox config maps
	if err := r.deleteExpiredSandboxConfigMaps(ctx); err != nil {
		return ctrl.Result{}, err
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
		nm := names.GenSandboxConfigMapName(r.Vdb, sb)
		cm := &corev1.ConfigMap{}
		err := r.Client.Get(ctx, nm, cm)
		if err != nil {
			r.Log.Error(err, "failed to retrieve sandbox config map", "configMapName", nm.Name)
			return err
		}
		cm.Annotations[vmeta.SandboxControllerUnsandboxTriggerID] = uuid.NewString()
		err = r.Client.Update(ctx, cm)
		if err != nil {
			r.Log.Error(err, "failed to update sandbox config map", "configMapName", nm.Name)
			return err
		}
		r.Log.Info("Successfully updated sandbox config map", "configMapName", nm.Name)
	}

	return nil
}

// deleteExpiredSandboxConfigMaps will delete sandbox config maps for non-existing sandboxes
func (r *UnsandboxSubclusterReconciler) deleteExpiredSandboxConfigMaps(ctx context.Context) error {
	cmList := &corev1.ConfigMapList{}
	namespace := r.Vdb.GetNamespace()
	err := r.Client.List(ctx, cmList, client.InNamespace(namespace))
	if err != nil {
		r.Log.Error(err, "failed to retrieve config map list")
		return err
	}
	existingSbs := make(map[string]any)
	for _, sb := range r.Vdb.Status.Sandboxes {
		existingSbs[sb.Name] = struct{}{}
	}
	for i := range cmList.Items {
		cm := &cmList.Items[i]
		// only process the config maps for unsandbox operation
		if cm.Annotations[vmeta.SandboxControllerUnsandboxTriggerID] != "" &&
			cm.Data[vapi.SandboxNameKey] != "" {
			if cm.Data[vapi.VerticaDBNameKey] != r.Vdb.Name {
				r.Log.Info("Vdb name in sandbox config map doesn't match current vdb's, skip processing this config map",
					"configMapName", cm.Name, "vdbNameInConfigMap", cm.Data[vapi.VerticaDBNameKey], "currentVdbName", r.Vdb.Name)
				continue
			}
			sb := cm.Data[vapi.SandboxNameKey]
			_, exists := existingSbs[sb]
			// delete sandbox config maps for the non-existing sandboxes
			if !exists {
				cmName := cm.Name
				err = r.Client.Delete(ctx, cm)
				if err != nil {
					r.Log.Error(err, "failed to delete sandbox config map", "configMapName", cmName)
					return err
				}
				r.Log.Info("deleted expired sandbox config map", "configMapName", cmName)
			}
		}
	}

	return nil
}
