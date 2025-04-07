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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/meta"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// TLSModeReconciler will update the tls modes when they are changed by users
type TLSModeReconciler struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	Log     logr.Logger
	PRunner cmds.PodRunner
	Pfacts  *podfacts.PodFacts
}

func MakeTLSModeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &TLSModeReconciler{
		VRec:    vdbrecon,
		Vdb:     vdb,
		Log:     log.WithName("TLSModeReconciler"),
		PRunner: prunner,
		Pfacts:  pfacts,
	}
}

// Reconcile will create a TLS secret for the http server if one is missing
func (h *TLSModeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !h.Vdb.IsCertRotationEnabled() {
		return ctrl.Result{}, nil
	}
	currentTLSMode := meta.GetNMAHTTPSPreviousTLSMode(h.Vdb.Annotations)
	newTLSMode := h.Vdb.Spec.NMATLSMode
	h.Log.Info("starting to tls mode reconcile, currentTLSMode - " + currentTLSMode + ", newTLSMode - " + newTLSMode)
	// this condition excludes bootstrap scenario
	if (newTLSMode != "" && currentTLSMode == "") || (newTLSMode != "" &&
		currentTLSMode != "" && newTLSMode == currentTLSMode) {
		return ctrl.Result{}, nil
	}
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.AlterNMATLSModeStarted,
		"Starting alter NMA TLS Mode to %s", h.Vdb.Spec.NMATLSMode)

	initiatorPod, ok := h.Pfacts.FindFirstUpPod(false, "")
	if !ok {
		h.Log.Info("No pod found to run vsql to alter tls mode. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	cmd := []string{
		"-c", fmt.Sprintf(`alter TLS CONFIGURATION https tlsmode '%s';`, h.Vdb.Spec.NMATLSMode),
	}
	_, stderr, err2 := h.PRunner.ExecVSQL(ctx, initiatorPod.GetName(), names.ServerContainer, cmd...)
	if err2 != nil || strings.Contains(stderr, "Error") {
		h.Log.Error(err2, "failed to execute TLS DDL to alter tls mode to "+h.Vdb.Spec.NMATLSMode+" stderr - "+stderr)
		return ctrl.Result{}, err2
	}
	chgs := vk8s.MetaChanges{
		NewAnnotations: map[string]string{
			vmeta.NMAHTTPSPreviousTLSMode: h.Vdb.Spec.NMATLSMode,
		},
	}
	if _, err := vk8s.MetaUpdate(ctx, h.VRec.Client, h.Vdb.ExtractNamespacedName(), h.Vdb, chgs); err != nil {
		return ctrl.Result{}, err
	}
	h.Log.Info("TLS DDL executed and TLS mode is set to " + h.Vdb.Spec.NMATLSMode)
	h.VRec.Eventf(h.Vdb, corev1.EventTypeNormal, events.AlterNMATLSModeSucceeded,
		"Successfully altered NMA TLS Mode to %s", h.Vdb.Spec.NMATLSMode)

	return ctrl.Result{}, nil
}
