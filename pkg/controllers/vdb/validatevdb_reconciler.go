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
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ValidateVDBReconciler will validate the vdb if operator upgraded
type ValidateVDBReconciler struct {
	VRec   config.ReconcilerInterface
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *podfacts.PodFacts
}

// MakeValidateVDBReconciler will build a ValidateVDBReconciler object
func MakeValidateVDBReconciler(vdbrecon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &ValidateVDBReconciler{
		VRec: vdbrecon,
		Log:  log.WithName("ValidateVDBReconciler"),
		Vdb:  vdb,
	}
}

func (r *ValidateVDBReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// No-op if no sandbox exists
	if r.Vdb.GenSandboxMap() == nil {
		return ctrl.Result{}, nil
	}

	if err := r.validateSubclusters(); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// validateSubclusters updates the vdb/sandbox subcluster type if needed
func (r *ValidateVDBReconciler) validateSubclusters() error {
	scsMain := []*vapi.Subcluster{}
	scsSandbox := []*vapi.SandboxSubcluster{}

	sbs := r.Vdb.GenSandboxMap()
	for sbName := range sbs {
		sb := sbs[sbName]
		scMap := r.Vdb.GenSubclusterMap()

		// round 1, to validate the vdb subcluster type
		for i := range sb.Subclusters {
			sc := scMap[sb.Subclusters[i].Name]
			if sc == nil {
				return fmt.Errorf("could not find subcluster %s", sb.Subclusters[i].Name)
			}

			// the vdb subcluster type is not valid only when operator upgraded from 25.2 to 25.3:
			// - vdb subcluster type is "sandboxprimary" (25.2 or earlier)
			// - sandbox subcluster type is not empty (25.3 or later) and
			if sb.Subclusters[i].Type != "" {
				if sc.Type == vapi.SandboxPrimarySubcluster {
					scsMain = append(scsMain, sc)
				} else {
					// the rest sandbox subclusters needs to be updated if not valid
					scsSandbox = append(scsSandbox, &sb.Subclusters[i])
				}
			}
		}

		// round 2, to update the vdb/sandbox subcluster type if not valid
		if len(scsMain) > 0 {
			// if the vdb subcluster type is not valid, we need to change the subcluster type to "secondary"
			for _, sc := range scsMain {
				sc.Type = vapi.SecondarySubcluster
			}
			// all the reset sandbox subclusters type should be "secondary"
			for _, sbSc := range scsSandbox {
				sbSc.Type = vapi.SecondarySubcluster
			}
		}
	}

	return nil
}
