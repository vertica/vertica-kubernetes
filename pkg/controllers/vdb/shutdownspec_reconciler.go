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
		scUpdate := false
		sc := &r.Vdb.Spec.Subclusters[i]
		sbName := scSbStatusMap[sc.Name]

		// Handle main cluster shutdown
		if sbName == v1.MainCluster {
			scUpdate = r.setSubclusterShutdownState(sc, sbName, r.Vdb.Spec.Shutdown)
			needUpdate = needUpdate || scUpdate
			continue
		}

		sb := r.Vdb.GetSandbox(sbName)
		scStatus := scStatusMap[sc.Name]

		// Skip invalid or untracked subclusters
		if sb == nil || scStatus == nil {
			continue
		}

		scUpdate = r.setSubclusterShutdownState(sc, sbName, sb.Shutdown)
		// If the subcluster is not in sync with the sandbox, we need to update
		needUpdate = needUpdate || scUpdate
	}
	return needUpdate, nil
}

// setSubclusterShutdownState sets the subcluster shutdown state based on the sandbox/main cluster shutdown state.
func (r *ShutdownSpecReconciler) setSubclusterShutdownState(sc *v1.Subcluster, sbName string, clusterShutdown bool) bool {
	needUpdate := false
	if sc.Annotations == nil && clusterShutdown {
		sc.Annotations = make(map[string]string, 1)
	}
	isClusterDrivenShutdown := vmeta.IsShutdownDrivenByMain(sc.Annotations)
	shutdownAnn := vmeta.ShutdownDrivenByMainAnnotation
	clusterName := "main cluster"
	if sbName != v1.MainCluster {
		isClusterDrivenShutdown = vmeta.IsShutdownDrivenBySandbox(sc.Annotations)
		shutdownAnn = vmeta.ShutdownDrivenBySandboxAnnotation
		clusterName = "sandbox"
	}
	// There are 3 cases to handle:
	switch {
	case clusterShutdown && !isClusterDrivenShutdown:
		// Add an annotation that indicates the shutdown/restart is controlled
		// by the sandbox/main as opposed to the subcluster. It helps
		// differentiate this case from when the user is explicitly
		// changing the subcluster's shutdown field.
		sc.Annotations[shutdownAnn] = vmeta.AnnotationTrue
		needUpdate = true
	case !clusterShutdown && isClusterDrivenShutdown:
		delete(sc.Annotations, shutdownAnn)
		needUpdate = true
	case !clusterShutdown && !isClusterDrivenShutdown:
		// Nothing to do if the sandbox/main is not shutdown and the
		// subcluster is not driven by the sandbox/main.
		return false
	}

	// Sync shutdown state with sandbox/main
	if clusterShutdown != sc.Shutdown {
		r.Log.Info("Syncing "+clusterName+" shutdown state to subcluster",
			"subcluster", sc.Name, "sandbox", sbName, "shutdown", clusterShutdown)
		// Update the subcluster shutdown field to match the sandbox
		sc.Shutdown = clusterShutdown
		needUpdate = true
	}
	return needUpdate
}
