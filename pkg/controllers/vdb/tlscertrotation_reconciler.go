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
	"strconv"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatenmacerts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSServerCertGenReconciler will create a secret that has TLS credentials.  This
// secret will be used to authenticate with the https server.
type TLSCertRoationReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSCertRotationReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSCertRoationReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSCertRoationReconciler"),
		Dispatcher: dispatcher,
		Pfacts:     pfacts,
	}
}

// Reconcile will rotate TLS certificate.
func (h *TLSCertRoationReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if vmeta.UseNMACertsMount(h.Vdb.Annotations) || !vmeta.EnableTLSCertsRotation(h.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}

	// this condition excludes bootstrap scenario
	if (h.Vdb.Spec.NMATLSSecret != "" && vmeta.GetNMATLSSecretName(h.Vdb.Annotations) == "") || (h.Vdb.Spec.NMATLSSecret != "" &&
		vmeta.GetNMATLSSecretName(h.Vdb.Annotations) != "" &&
		h.Vdb.Spec.NMATLSSecret == vmeta.GetNMATLSSecretName(h.Vdb.Annotations)) {
		return ctrl.Result{}, nil
	}
	// rotation is required. Will check start conditions next
	// check if secret is ready for rotation
	nmaSecret := corev1.Secret{}
	found, err := vapi.IsK8sSecretFound(ctx, h.Vdb, h.VRec.Client, &h.Vdb.Spec.NMATLSSecret, &nmaSecret)
	if !found || err != nil {
		h.Log.Info("nma secret is not ready yet for rotation. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	// check if configmap is ready for rotation
	name := fmt.Sprintf("%s-%s", h.Vdb.Name, vapi.NMATLSConfigMapName)
	configMapName := types.NamespacedName{
		Name:      name,
		Namespace: h.Vdb.GetNamespace(),
	}
	configMap := &corev1.ConfigMap{}
	err = h.VRec.Client.Get(ctx, configMapName, configMap)
	if err != nil {
		h.Log.Info("failed to retrieve configmap for rotation. will retry")
		return ctrl.Result{Requeue: true}, nil
	}
	if configMap.Data[builder.NMASecretNamespaceEnv] != h.Vdb.GetObjectMeta().GetNamespace() ||
		configMap.Data[builder.NMASecretNameEnv] != h.Vdb.Spec.NMATLSSecret {
		h.Log.Info("new nma secret name not found in configmap. cert rotation will not start")
		return ctrl.Result{Requeue: true}, nil
	}
	// Now nma cert rotation will start
	return h.rotateNmaTLSCert(ctx, &nmaSecret)
}

func (h *TLSCertRoationReconciler) rotateNmaTLSCert(ctx context.Context, nmaSecret *corev1.Secret) (ctrl.Result, error) {
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run rotate nma cert. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	secretName := meta.GetNMATLSSecretName(h.Vdb.Annotations)
	h.Log.Info("libo: to rotate certi from " + secretName + " to " + h.Vdb.Spec.NMATLSSecret)
	opts := []rotatenmacerts.Option{
		rotatenmacerts.WithKey(string(nmaSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatenmacerts.WithCert(string(nmaSecret.Data[corev1.TLSCertKey])),
		rotatenmacerts.WithCaCert(string(nmaSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatenmacerts.WithInitiator(initiatorPod.GetPodIP()),
	}
	vdbContext := vadmin.GetContextForVdb(h.Vdb.Namespace, h.Vdb.Name)
	h.Log.Info("libo: to call RotateNMACerts, use tls " + strconv.FormatBool(vdbContext.GetBoolValue(vadmin.UseTLSCert)))
	err := h.Dispatcher.RotateNMACerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate nma cer to "+h.Vdb.Spec.NMATLSSecret)
		return ctrl.Result{}, err
	}
	h.Log.Info("cert has been rotated to " + h.Vdb.Spec.NMATLSSecret)
	chgs := vk8s.MetaChanges{
		NewAnnotations: map[string]string{
			vmeta.NMATLSSECRETAnnotation: h.Vdb.Spec.NMATLSSecret,
		},
	}
	h.Log.Info("libo: to save secret in annotation")
	if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {
		h.Log.Error(err, "failed to update secret name in annotation after cert rotation")
		return ctrl.Result{}, err
	}
	h.Log.Info("rotated cert has been saved in annotation - " + h.Vdb.Spec.NMATLSSecret)
	return ctrl.Result{}, nil
}
