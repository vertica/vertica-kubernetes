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
	"slices"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
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

	scsMain, scsSandbox, err := r.validateSubclusters()
	if err != nil || len(scsMain) == 0 {
		return ctrl.Result{}, err
	}

	return r.updateSubclusters(ctx, scsMain, scsSandbox)
}

// validateSubclusters updates the vdb/sandbox subcluster type if needed
func (r *ValidateVDBReconciler) validateSubclusters() (scsMain, scsSandbox []string, err error) {
	sbMap := r.Vdb.GenSandboxMap()
	scMap := r.Vdb.GenSubclusterMap()
	for sbName := range sbMap {
		sb := sbMap[sbName]
		// to find the subcluster that needs to be updated
		for i := range sb.Subclusters {
			sc := scMap[sb.Subclusters[i].Name]
			if sc == nil {
				return scsMain, scsSandbox, fmt.Errorf("could not find subcluster %s", sb.Subclusters[i].Name)
			}

			// the vdb subcluster type is not valid only when upgrade happens:
			// - vdb subcluster type is "sandboxprimary"
			// - sandbox subcluster type is not empty
			if sb.Subclusters[i].Type != "" {
				if sc.Type == vapi.SandboxPrimarySubcluster {
					r.Log.Info("found subcluster to be updated", "subcluster", sc.Name,
						"subcluster type", sc.Type, "sandbox subcluster type", sb.Subclusters[i].Type)
					scsMain = append(scsMain, sc.Name)
				} else {
					// the rest sandbox subclusters needs to be updated to "secondary" if not valid
					scsSandbox = append(scsSandbox, sb.Subclusters[i].Name)
				}
			}
		}
	}

	return scsMain, scsSandbox, nil
}

// validateSubclusters updates the vdb/sandbox subcluster type if needed
func (r *ValidateVDBReconciler) updateSubclusters(ctx context.Context, scsMain, scsSandbox []string) (ctrl.Result, error) {
	// update the sandbox subcluster type only if sandboxprimary found in scsMain
	if len(scsMain) == 0 {
		return ctrl.Result{}, nil
	}

	// to update the vdb/sandbox subcluster type if not valid:
	// 1. if the vdb subcluster type is not valid, we need to change the subcluster type to "secondary"
	for _, scName := range scsMain {
		_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.Vdb, func() (bool, error) {
			for j := range r.Vdb.Spec.Subclusters {
				if r.Vdb.Spec.Subclusters[j].Name == scName &&
					r.Vdb.Spec.Subclusters[j].Type == vapi.SandboxPrimarySubcluster {
					r.Log.Info("update subcluster type", "subcluster", scName,
						"old type", vapi.SandboxPrimarySubcluster, "new type", vapi.SecondarySubcluster)
					r.Vdb.Spec.Subclusters[j].Type = vapi.SecondarySubcluster
				}
			}
			return true, nil
		})
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// 2. if the vdb subcluster type is not valid, we need to change the sandbox subcluster type accordingly
	_, err := vk8s.UpdateVDBWithRetry(ctx, r.VRec, r.Vdb, func() (bool, error) {
		for j := range r.Vdb.Spec.Sandboxes {
			sandbox := &r.Vdb.Spec.Sandboxes[j]
			for k := range sandbox.Subclusters {
				subcluster := &sandbox.Subclusters[k]
				// make sure the primary subcluster type is "primary"
				if slices.Contains(scsMain, subcluster.Name) &&
					subcluster.Type != vapi.PrimarySubcluster {
					r.Log.Info("update sandbox subcluster type to primary", "sandbox", sandbox.Name,
						"subcluster", subcluster.Name, "old type", subcluster.Type, "new type", vapi.PrimarySubcluster)
					subcluster.Type = vapi.PrimarySubcluster
				}
				// make sure the secondary subcluster type is "secondary"
				if slices.Contains(scsSandbox, subcluster.Name) &&
					subcluster.Type != vapi.SecondarySubcluster {
					r.Log.Info("update sandbox subcluster type to secondary", "sandbox", sandbox.Name,
						"subcluster", subcluster.Name, "old type", subcluster.Type, "new type", vapi.SecondarySubcluster)
					subcluster.Type = vapi.SecondarySubcluster
				}
			}
		}
		return true, nil
	})
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
