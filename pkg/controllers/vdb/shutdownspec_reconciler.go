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
}

func MakeShutdownSpecReconciler(r config.ReconcilerInterface,
	vdb *v1.VerticaDB) controllers.ReconcileActor {
	return &ShutdownSpecReconciler{
		VRec: r,
		Vdb:  vdb,
	}
}

func (r *ShutdownSpecReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op as there is no sandbox
	if len(r.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}
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
	scMap := r.Vdb.GenSubclusterMap()
	scStatusMap := r.Vdb.GenSubclusterStatusMap()
	for i := range r.Vdb.Spec.Sandboxes {
		sb := &r.Vdb.Spec.Sandboxes[i]
		sbStatus := r.Vdb.GetSandboxStatus(sb.Name)
		if sbStatus == nil {
			continue
		}
		for j := range sb.Subclusters {
			scName := sb.Subclusters[j].Name
			sc := scMap[scName]
			scStatus := scStatusMap[scName]
			if !sbStatus.IsSubclusterInSandbox(scName) || scStatus == nil {
				break
			}

			if sb.Shutdown {
				if sc.Annotations == nil {
					sc.Annotations = make(map[string]string, 1)
				}
				if _, ok := sc.Annotations[vmeta.ShutdownDrivenBySandbox]; !ok {
					// Add a label that indicate the shutdown/restart is controlled
					// by the sandbox as opposed to the subcluster. It helps
					// differentiate this case from when the user is explicitly
					// changing the subcluster's shutdown field.
					sc.Annotations[vmeta.ShutdownDrivenBySandbox] = "true"
					needUpdate = true
				}
			} else {
				// If the shutdown/restart is not controlled by the sandbox,
				// we skip to the next subcluster.
				if !vmeta.GetShutdownDrivenBySandbox(sc.Annotations) {
					continue
				}
				delete(sc.Annotations, vmeta.ShutdownDrivenBySandbox)
				needUpdate = true
			}
			if sb.Shutdown != sc.Shutdown {
				sc.Shutdown = sb.Shutdown
				needUpdate = true
			}
		}
	}
	return needUpdate, nil
}
