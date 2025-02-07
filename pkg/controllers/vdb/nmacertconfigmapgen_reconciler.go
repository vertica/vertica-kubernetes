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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the http server.
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

// Reconcile will create a TLS secret for the http server if one is missing
func (h *NMACertConfigMapGenReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
<<<<<<< HEAD
	if !h.tlsSecretsReady(ctx) {
		return ctrl.Result{Requeue: true}, nil
	}
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
	h.Log.Info("libo: config map name - " + name)
=======
	nmaSecret := corev1.Secret{}
	if !h.tlsSecretsReady(ctx, &nmaSecret) {
		return ctrl.Result{Requeue: true}, nil
	}
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
>>>>>>> VER-99110
	configMapName := types.NamespacedName{
		Name:      name,
		Namespace: h.Vdb.GetNamespace(),
	}
	configMap := &corev1.ConfigMap{}
	err := h.VRec.Client.Get(ctx, configMapName, configMap)
<<<<<<< HEAD
	if errors.IsNotFound(err) {
		configMap = builder.BuildNMATLSConfigMap(name, h.Vdb)
		err = h.VRec.Client.Create(ctx, configMap)
		return ctrl.Result{}, err
	}
	h.Log.Info("created TLS cert secret configmap", "nm", configMapName.Name)
=======
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
>>>>>>> VER-99110
	return ctrl.Result{}, err
}

// tlsSecretsReady returns true when all TLS secrets are found in k8s env
<<<<<<< HEAD
func (h *NMACertConfigMapGenReconciler) tlsSecretsReady(ctx context.Context) bool {
=======
func (h *NMACertConfigMapGenReconciler) tlsSecretsReady(ctx context.Context, secret *corev1.Secret) bool {
>>>>>>> VER-99110
	if h.Vdb.Spec.NMATLSSecret == "" {
		h.Log.Info("nma secret name is not ready. wait for it to be created")
		return false
	}
<<<<<<< HEAD
	found, err := vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &h.Vdb.Spec.NMATLSSecret)
=======
	found, err := vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &h.Vdb.Spec.NMATLSSecret, secret)
>>>>>>> VER-99110
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
