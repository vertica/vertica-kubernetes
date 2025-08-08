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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	ctrl "sigs.k8s.io/controller-runtime"
)

// LabelsForReferencedObjsReconciler is a reconciler that applies labels, of
// configmaps/secrets used for backing up env vars, to the VerticaDB
type LabelsForReferencedObjsReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeLabelsForReferencedObjsReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &LabelsForReferencedObjsReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("LabelsForReferencedObjsReconciler"),
	}
}

func (l *LabelsForReferencedObjsReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	secrets := l.getreferencedSecret()
	configmaps := l.getreferencedConfigmap()
	labels := map[string]string{}
	if len(secrets) > 0 {
		labels[vmeta.SecretSelectorLabel] = strings.Join(secrets, ",")
	}
	if len(configmaps) > 0 {
		labels[vmeta.ConfigMapSelectorLabel] = strings.Join(configmaps, ",")
	}
	if len(labels) > 0 {
		return ctrl.Result{}, l.applyLabels(ctx, labels)
	}

	return ctrl.Result{}, nil
}

// applyLabels applies the labels whose values are a comma-separated list of configmap/secret names used
// to pass env vars to the VerticaDB.
func (l *LabelsForReferencedObjsReconciler) applyLabels(ctx context.Context, labels map[string]string) error {
	chgs := vk8s.MetaChanges{
		NewLabels: labels,
	}

	updated, err := vk8s.MetaUpdate(ctx, l.VRec.GetClient(), l.Vdb.ExtractNamespacedName(), l.Vdb, chgs)
	if updated {
		l.Log.Info("Applied env vars' configmap/secret labels to vdb", "vdbName", l.Vdb.Name)
	}
	return err
}

func (l *LabelsForReferencedObjsReconciler) getreferencedResources(isSecret bool) []string {
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

	return resourceSlice
}

// // getreferencedConfigmap returns a list of configmap names used to pass env vars to the VerticaDB.
func (l *LabelsForReferencedObjsReconciler) getreferencedConfigmap() []string {
	return l.getreferencedResources(false)
}

// getreferencedSecret returns a list of secret names used to pass env	vars to the VerticaDB.
func (l *LabelsForReferencedObjsReconciler) getreferencedSecret() []string {
	return l.getreferencedResources(true)
}
