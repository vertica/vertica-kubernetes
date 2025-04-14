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
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/rotatehttpscerts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSModeReconciler will update the tls modes when they are changed by users
type TLSModeReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log        logr.Logger
	PRunner    cmds.PodRunner
	Dispatcher vadmin.Dispatcher
	Pfacts     *podfacts.PodFacts
}

func MakeTLSModeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner,
	dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSModeReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		Log:        log.WithName("TLSModeReconciler"),
		Dispatcher: dispatcher,
		PRunner:    prunner,
		Pfacts:     pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSModeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsCertRotationEnabled() || h.Vdb.IsStatusConditionTrue(vapi.TLSCertRotationInProgress) ||
		!h.Vdb.IsStatusConditionTrue(vapi.DBInitialized) {
		return ctrl.Result{}, nil
	}
	currentTLSMode := vmeta.GetNMAHTTPSPreviousTLSMode(h.Vdb.Annotations)
	newTLSMode := h.Vdb.Spec.HTTPSTLSMode
	h.Log.Info("starting to reconcile https tls mode, current TLS mode - " + currentTLSMode + ", new TLS mode - " + newTLSMode)
	// this condition excludes bootstrap scenario
	if currentTLSMode == "" || newTLSMode == currentTLSMode {
		return ctrl.Result{}, nil
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSModeUpdateStarted,
		"Starting to update HTTPS TLS Mode to %s", newTLSMode)
	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run vsql to update https tls mode. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	currentSecretName := vmeta.GetNMATLSSecretNameInUse(h.Vdb.Annotations)
	nmCurrentSecretName := types.NamespacedName{
		Name:      currentSecretName,
		Namespace: h.Vdb.GetNamespace(),
	}
	secretFetcher := &cloud.SecretFetcher{
		Client:   h.VRec.Client,
		Log:      h.Log,
		EVWriter: h.VRec,
		Obj:      h.Vdb,
	}
	currentSecretData, res, err := secretFetcher.FetchAllowRequeue(ctx, nmCurrentSecretName)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	currentSecret := &corev1.Secret{
		Data: currentSecretData,
	}

	h.Log.Info("ready to change HTTPS TLS mode from " + currentTLSMode + " to " + newTLSMode)
	currentCert := string(currentSecret.Data[corev1.TLSCertKey])
	keyConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSPrivateKeyKey, h.Vdb.Namespace)
	certConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", corev1.TLSCertKey, h.Vdb.Namespace)
	caCertConfig := fmt.Sprintf("{\"data-key\":%q, \"namespace\":%q}", paths.HTTPServerCACrtName, h.Vdb.Namespace)
	opts := []rotatehttpscerts.Option{
		rotatehttpscerts.WithPollingKey(string(currentSecret.Data[corev1.TLSPrivateKeyKey])),
		rotatehttpscerts.WithPollingCert(currentCert),
		rotatehttpscerts.WithPollingCaCert(string(currentSecret.Data[corev1.ServiceAccountRootCAKey])),
		rotatehttpscerts.WithKey(h.Vdb.Spec.NMATLSSecret, keyConfig),
		rotatehttpscerts.WithCert(h.Vdb.Spec.NMATLSSecret, certConfig),
		rotatehttpscerts.WithCaCert(h.Vdb.Spec.NMATLSSecret, caCertConfig),
		rotatehttpscerts.WithTLSMode(newTLSMode),
		rotatehttpscerts.WithInitiator(initiatorPod.GetPodIP()),
	}
	h.Log.Info("call RotateHTTPSCerts for cert - " + h.Vdb.Spec.NMATLSSecret + ", tls mode - " + newTLSMode +
		", tls enabled " + strconv.FormatBool(h.Vdb.IsCertRotationEnabled()))
	err = h.Dispatcher.RotateHTTPSCerts(ctx, opts...)
	if err != nil {
		h.Log.Error(err, "failed to rotate https cer to "+h.Vdb.Spec.NMATLSSecret)
		return ctrl.Result{Requeue: true}, err
	}

	chgs := vk8s.MetaChanges{
		NewAnnotations: map[string]string{
			vmeta.NMAHTTPSPreviousTLSMode: newTLSMode,
		},
	}
	if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info("TLS DDL executed and HTTPS TLS mode is updated to " + newTLSMode)
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.NMATLSModeUpdateSucceeded,
		"Successfully updated HTTPS TLS Mode to %s", newTLSMode)
	return ctrl.Result{}, nil
}
