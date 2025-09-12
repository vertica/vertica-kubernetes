/*
 (c) Copyright [2021-2025] Open Text.
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
	"slices"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ObservedConfigObjsReconciler will update the status fields ObservedConfigMaps
// and ObservedSecrets in the VerticaDB CRD.  These fields are used to keep track
// of configmaps and secrets referenced by the vdb in spec.envFrom and spec.extraEnv.
type ObservedConfigObjsReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeObservedConfigObjsReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &ObservedConfigObjsReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("ObservedConfigObjsReconciler"),
	}
}

func (l *ObservedConfigObjsReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	secrets := l.getReferencedSecret()
	configmaps := l.getReferencedConfigmap()
	if len(secrets) > 0 || len(configmaps) > 0 {
		if equalSets(secrets, l.Vdb.Status.ObservedSecrets) && equalSets(configmaps, l.Vdb.Status.ObservedConfigMaps) {
			// Nothing has changed, so no need to update labels or status.
			return ctrl.Result{}, nil
		}
		// Update the status with the latest configmaps/secrets used for env vars.
		if err := vdbstatus.UpdateObservedConfigObjects(ctx, l.VRec.GetClient(), l.Vdb, configmaps, secrets); err != nil {
			l.Log.Error(err, "Failed to update status with observed configmaps/secrets", "vdbName", l.Vdb.Name)
			return ctrl.Result{}, err
		}
		l.Log.Info("Applied env vars' configmap/secret labels to vdb", "vdbName", l.Vdb.Name)
	}

	return ctrl.Result{}, nil
}

func (l *ObservedConfigObjsReconciler) getReferencedResources(isSecret bool) []string {
	resourceSet := map[string]struct{}{}

	// Handle ExtraEnv
	for i := range l.Vdb.Spec.ExtraEnv {
		env := &l.Vdb.Spec.ExtraEnv[i]

		if env.ValueFrom != nil {
			if isSecret && env.ValueFrom.SecretKeyRef != nil {
				resourceSet[env.ValueFrom.SecretKeyRef.Name] = struct{}{}
			}
			if !isSecret && env.ValueFrom.ConfigMapKeyRef != nil {
				resourceSet[env.ValueFrom.ConfigMapKeyRef.Name] = struct{}{}
			}
		}
	}

	// Handle EnvFrom
	for i := range l.Vdb.Spec.EnvFrom {
		envFrom := &l.Vdb.Spec.EnvFrom[i]

		if isSecret && envFrom.SecretRef != nil {
			resourceSet[envFrom.SecretRef.Name] = struct{}{}
		}
		if !isSecret && envFrom.ConfigMapRef != nil {
			resourceSet[envFrom.ConfigMapRef.Name] = struct{}{}
		}
	}

	// Convert set to slice
	resourceSlice := make([]string, 0, len(resourceSet))
	for key := range resourceSet {
		resourceSlice = append(resourceSlice, key)
	}

	// Sort the slice to make the order deterministic
	slices.Sort(resourceSlice)

	return resourceSlice
}

// // getreferencedConfigmap returns a list of configmap names used to pass env vars to the VerticaDB.
func (l *ObservedConfigObjsReconciler) getReferencedConfigmap() []string {
	return l.getReferencedResources(false)
}

// getreferencedSecret returns a list of secret names used to pass env vars to the VerticaDB.
func (l *ObservedConfigObjsReconciler) getReferencedSecret() []string {
	return l.getReferencedResources(true)
}

func equalSets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	m := make(map[string]struct{}, len(a))
	for _, v := range a {
		m[v] = struct{}{}
	}

	for _, v := range b {
		if _, ok := m[v]; !ok {
			return false
		}
	}

	return true
}
