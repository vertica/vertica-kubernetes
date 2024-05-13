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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SandboxImageReconciler will set the image, if missing,
// for sandboxes
type SandboxImageReconciler struct {
	VRec *VerticaDBReconciler
	Log  logr.Logger
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on
}

func MakeSandboxImageReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &SandboxImageReconciler{
		VRec: vdbrecon,
		Log:  log.WithName("SandboxImageReconciler"),
		Vdb:  vdb,
	}
}

func (s *SandboxImageReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}
	_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, s.setSandboxImagesInVdb)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed trying to update sandbox images in VDB: %w", err)
	}
	return ctrl.Result{}, nil
}

// setSandboxImagesInVdb is a callback function for updateVDBWithRetry
// that will set sandbox images(if empty) in vdb.
func (s *SandboxImageReconciler) setSandboxImagesInVdb() (bool, error) {
	updated := false
	for i := range s.Vdb.Spec.Sandboxes {
		sb := &s.Vdb.Spec.Sandboxes[i]
		if sb.Image == "" {
			updated = true
			sb.Image = s.Vdb.Spec.Image
		}
	}
	return updated, nil
}
