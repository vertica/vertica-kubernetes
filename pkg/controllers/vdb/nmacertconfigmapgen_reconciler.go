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
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertConfigMapGenReconciler will create a configmap that has the nma secret's name
// and namespace in it. They will be mapped to two environmental variables in NMA container
type NMACertConfigMapGenReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeNMACertConfigMapGenReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &NMACertConfigMapGenReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("TLSCertConfigMapGenReconciler"),
	}
}

// Reconcile will create a TLS secret for the https server if one is missing
func (h *NMACertConfigMapGenReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if vmeta.UseNMACertsMount(h.Vdb.Annotations) || !vmeta.EnableTLSCertsRotation(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}
	nmaSecret := corev1.Secret{}
	if !h.tlsSecretsReady(ctx, &nmaSecret) {
		h.Log.Info("nma secret is not ready yet to create configmap. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
	configMapName := types.NamespacedName{
		Name:      name,
		Namespace: h.Vdb.GetNamespace(),
	}
	configMap := &corev1.ConfigMap{}
	err := h.VRec.Client.Get(ctx, configMapName, configMap)
	if err != nil {
		if errors.IsNotFound(err) {
			configMap = builder.BuildNMATLSConfigMap(name, h.Vdb)
			err = h.VRec.Client.Create(ctx, configMap)
			h.Log.Info("created TLS cert secret configmap", "nm", configMapName.Name)
			return ctrl.Result{}, err
		}
		h.Log.Info("failed to retrieve TLS cert secret configmap", "nm", configMapName.Name)
		return ctrl.Result{}, err
	}
	if configMap.Data[builder.NMASecretNamespaceEnv] != h.Vdb.GetObjectMeta().GetNamespace() ||
		configMap.Data[builder.NMASecretNameEnv] != h.Vdb.Spec.NMATLSSecret {
		configMap = builder.BuildNMATLSConfigMap(name, h.Vdb)
		err = h.VRec.Client.Update(ctx, configMap)
		h.Log.Info("config map " + name + " is updated for new nma secret " + h.Vdb.Spec.NMATLSSecret)
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
}

// tlsSecretsReady returns true when all TLS secrets are found in k8s env
func (h *NMACertConfigMapGenReconciler) tlsSecretsReady(ctx context.Context, secret *corev1.Secret) bool {
	if h.Vdb.Spec.NMATLSSecret == "" {
		h.Log.Info("nma secret name is not ready. wait for it to be created")
		return false
	}
	found, err := vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &h.Vdb.Spec.NMATLSSecret, secret)
	if !found || err != nil {
		if err == nil {
			h.Log.Info("did not find nma tls secret " + h.Vdb.Spec.NMATLSSecret)
		} else {
			h.Log.Info("failed to find nma tls secret " + h.Vdb.Spec.NMATLSSecret + " because of err: " + err.Error())
		}

		return false
	}
	return true
}
