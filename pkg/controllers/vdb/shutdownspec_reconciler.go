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
	v1 "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ShutdownSpecReconciler will make sure that subclusters, part of a sandbox,
// that needs to be shutdown/restarted, have the correct shutdown value.
type ShutdownSpecReconciler struct {
	VRec config.ReconcilerInterface
	Vdb  *v1.VerticaDB
	Log  logr.Logger
}

func MakeShutdownSpecReconciler(r config.ReconcilerInterface,
	vdb *v1.VerticaDB, log logr.Logger) controllers.ReconcileActor {
	return &ShutdownSpecReconciler{
		VRec: r,
		Vdb:  vdb,
		Log:  log.WithName("ShutdownSpecReconciler"),
	}
}

func (r *ShutdownSpecReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	return ctrl.Result{}, r.updateSubclustersShutdownState(ctx)
}

// updateSubclustersShutdownState updates the sandbox's subclusters shutdown state after
// the sandbox has been restarted.
func (r *ShutdownSpecReconciler) updateSubclustersShutdownState(ctx context.Context) error {
	_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.Vdb, r.updateSubclustersShutdownStateCallback)
	return err
}

func (r *ShutdownSpecReconciler) updateSubclustersShutdownStateCallback() (bool, error) {
	needUpdate := false

	scStatusMap := r.Vdb.GenSubclusterStatusMap()
	scSbStatusMap := r.Vdb.GenSubclusterSandboxStatusMap()

	for i := range r.Vdb.Spec.Subclusters {
		sc := &r.Vdb.Spec.Subclusters[i]
		sbName := scSbStatusMap[sc.Name]

		// Handle main cluster shutdown
		if sbName == v1.MainCluster {
			// Sync shutdown state with main cluster
			if r.Vdb.Spec.Shutdown != sc.Shutdown {
				r.Log.Info("Syncing main cluster shutdown state to subcluster",
					"subcluster", sc.Name, "shutdown", r.Vdb.Spec.Shutdown)
				sc.Shutdown = r.Vdb.Spec.Shutdown
				needUpdate = true
			}
			continue
		}

		sb := r.Vdb.GetSandbox(sbName)
		scStatus := scStatusMap[sc.Name]

		// Skip invalid or untracked subclusters
		if sb == nil || scStatus == nil {
			continue
		}

		// Ensure annotations map exists when needed
		if sc.Annotations == nil && sb.Shutdown {
			sc.Annotations = make(map[string]string, 1)
		}

		// Sync shutdown-driven-by-sandbox annotation
		drivenBySandbox := vmeta.GetShutdownDrivenBySandbox(sc.Annotations)

		switch {
		case sb.Shutdown && !drivenBySandbox:
			// Add an annotation that indicates the shutdown/restart is controlled
			// by the sandbox as opposed to the subcluster. It helps
			// differentiate this case from when the user is explicitly
			// changing the subcluster's shutdown field.
			sc.Annotations[vmeta.ShutdownDrivenBySandbox] = vmeta.AnnotationTrue
			needUpdate = true
		case !sb.Shutdown && drivenBySandbox:
			delete(sc.Annotations, vmeta.ShutdownDrivenBySandbox)
			needUpdate = true
		}

		// Sync shutdown state with sandbox
		if sb.Shutdown != sc.Shutdown {
			r.Log.Info("Syncing sandbox shutdown state to subcluster",
				"subcluster", sc.Name, "sandbox", sb.Name, "shutdown", sb.Shutdown)
			// Update the subcluster shutdown field to match the sandbox
			sc.Shutdown = sb.Shutdown
			needUpdate = true
		}
	}

	return needUpdate, nil
}
