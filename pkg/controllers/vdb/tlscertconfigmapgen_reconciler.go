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
	"encoding/json"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the http server.
type TLSCertConfigMapGenReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

type secretNames struct {
	HttpsTLSSecret  string `json:"httpsTLSSecret"`
	ClientTLSSecret string `json:"clientTLSSecret"`
}

func MakeTLSCertConfigMapGenReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &TLSCertConfigMapGenReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("TLSCertConfigMapGenReconciler"),
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSCertConfigMapGenReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {

	if h.Vdb.Spec.NMATLSSecret == "" || h.Vdb.Spec.HttpsTLSSecret == "" ||
		h.Vdb.Spec.ClientTLSSecret == "" {
		h.Log.Info("not all tls secrets are ready. wait to create tls cert configmap")
		return ctrl.Result{Requeue: true}, nil
	}
	jsonBytes, err := h.buildJsonBytes(h.Vdb)
	if err != nil {
		h.Log.Error(err, "failed to serialize secretNames")
		return ctrl.Result{}, err
	}

	configMapName := names.GenNamespacedName(h.Vdb, vapi.TLSConfigMapName)
	configMap := &corev1.ConfigMap{}
	err = h.VRec.Client.Get(ctx, configMapName, configMap)
	if errors.IsNotFound(err) {
		configMap = h.buildTLSConfigMap(string(jsonBytes), h.Vdb)
		err = h.VRec.Client.Create(ctx, configMap)
		return ctrl.Result{}, err
	}
	h.Log.Info("created TLS cert secret configmap")
	return ctrl.Result{}, err
}

// buildJsonBytes serializes the struct of secret names
func (h *TLSCertConfigMapGenReconciler) buildJsonBytes(vdb *vapi.VerticaDB) ([]byte, error) {
	scretNames := secretNames{
		HttpsTLSSecret:  vdb.Spec.HttpsTLSSecret,
		ClientTLSSecret: vdb.Spec.ClientTLSSecret,
	}
	return json.Marshal(scretNames)
}

// buildTLSConfigMap return a ConfigMap. Key is the json file name and value is the json file content
func (h *TLSCertConfigMapGenReconciler) buildTLSConfigMap(jsonContent string, vdb *vapi.VerticaDB) *corev1.ConfigMap {
	jsonMap := map[string]string{
		"tlsSecretsConfig.json": jsonContent,
	}
	tlsConfigMap := &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            vapi.TLSConfigMapName,
			Namespace:       vdb.Namespace,
			OwnerReferences: []metav1.OwnerReference{h.Vdb.GenerateOwnerReference()},
		},
		Data: jsonMap,
	}
	return tlsConfigMap
}
