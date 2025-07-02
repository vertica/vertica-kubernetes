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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
)

// NMACertConfigMapGenReconciler will create a configmap that has the nma secret's name
// and namespace in it. They will be mapped to two environmental variables in NMA container
type NMACertConfigMapReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log  logr.Logger
}

func MakeNMACertConfigMapReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB) controllers.ReconcileActor {
	return &NMACertConfigMapReconciler{
		VRec: vdbrecon,
		Vdb:  vdb,
		Log:  log.WithName("NMACertConfigMapReconciler"),
	}
}

// Reconcile() will create a configmap whose values are mapped to environmental variables in NMA container
func (h *NMACertConfigMapReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Do not update NMA config map when a rollback is required
	if h.Vdb.IsTLSCertRollbackNeeded() {
		return ctrl.Result{}, nil
	}

	configMapName := names.GenNMACertConfigMap(h.Vdb)
	configMap := &corev1.ConfigMap{}
	err := h.VRec.GetClient().Get(ctx, configMapName, configMap)
	if err != nil {
		if kerrors.IsNotFound(err) {
			configMap = builder.BuildNMATLSConfigMap(configMapName, h.Vdb)
			err = h.VRec.GetClient().Create(ctx, configMap)
			if err != nil {
				return ctrl.Result{}, err
			}
			h.Log.Info("created TLS cert secret configmap", "nm", configMapName.Name)
			return ctrl.Result{}, nil
		}
		h.Log.Error(err, "failed to retrieve TLS cert secret configmap")
		return ctrl.Result{}, err
	}
	if !h.Vdb.IsSetForTLS() {
		return ctrl.Result{}, nil
	}
	if configMap.Data[builder.NMASecretNameEnv] == h.Vdb.GetHTTPSNMATLSSecret() &&
		configMap.Data[builder.NMAClientSecretNameEnv] == h.Vdb.GetClientServerTLSSecret() &&
		configMap.Data[builder.NMASecretNamespaceEnv] == h.Vdb.ObjectMeta.Namespace &&
		configMap.Data[builder.NMAClientSecretNamespaceEnv] == h.Vdb.ObjectMeta.Namespace &&
		configMap.Data[builder.NMAClientSecretTLSModeEnv] == h.Vdb.GetNMAClientServerTLSMode() {
		return ctrl.Result{}, nil
	}

	configMap.Data[builder.NMASecretNameEnv] = h.Vdb.GetHTTPSNMATLSSecret()
	configMap.Data[builder.NMASecretNamespaceEnv] = h.Vdb.ObjectMeta.Namespace
	configMap.Data[builder.NMAClientSecretNameEnv] = h.Vdb.GetClientServerTLSSecret()
	configMap.Data[builder.NMAClientSecretNamespaceEnv] = h.Vdb.ObjectMeta.Namespace
	configMap.Data[builder.NMAClientSecretTLSModeEnv] = h.Vdb.GetNMAClientServerTLSMode()

	err = h.VRec.GetClient().Update(ctx, configMap)
	if err == nil {
		h.Log.Info("updated tls cert secret configmap", "name", configMapName.Name, "nma-secret", h.Vdb.GetHTTPSNMATLSSecret(),
			"clientserver-secret", h.Vdb.GetClientServerTLSSecret())
	}
	return ctrl.Result{}, err
}
