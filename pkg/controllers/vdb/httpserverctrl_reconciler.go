/*
 (c) Copyright [2021-2023] Open Text.
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

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// HTTPServerCtrlReconciler will start the http server if needed.
type HTTPServerCtrlReconciler struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

func MakeHTTPServerCtrlReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &HTTPServerCtrlReconciler{
		VRec:    vdbrecon,
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
	}
}

func (h *HTTPServerCtrlReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Early out if the http service isn't enabled.
	if !h.doHTTPStart(true) {
		return ctrl.Result{}, nil
	}

	if err := h.PFacts.Collect(ctx, h.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	for _, pod := range h.PFacts.Detail {
		if pod.isHTTPServerRunning {
			continue
		}
		// Only start the http server for pods that have been added to a database
		if !pod.upNode {
			h.VRec.Log.Info("Skipping http server start on pod because vertica is not running", "pod", pod.name)
			continue
		}
		// We need this config file for the http server.
		if !pod.fileExists[paths.HTTPTLSConfFile] {
			h.VRec.Log.Info("Skipping http server start because https config is still missing in pod", "pod", pod.name)
			continue
		}
		if err := h.startHTTPServerInPod(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}

		// Invalidate the pod facts cache because we started the http server
		h.PFacts.Invalidate()
	}

	return ctrl.Result{}, nil
}

// startHTTPServerInPod will start the http server in the given pod
func (h *HTTPServerCtrlReconciler) startHTTPServerInPod(ctx context.Context, pod *PodFact) error {
	cmd := []string{
		"-tAc", genHTTPServerCtrlQuery("start"),
	}
	h.VRec.Event(h.Vdb, corev1.EventTypeNormal, events.HTTPServerStartStarted,
		fmt.Sprintf("Starting http server in pod %s", pod.name))
	if _, _, err := h.PRunner.ExecVSQL(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
		h.VRec.Event(h.Vdb, corev1.EventTypeWarning, events.HTTPServerStartFailed,
			fmt.Sprintf("Failed to start the http server in pod %s", pod.name))
		return err
	}
	return nil
}

// doHTTPStart returns true if an attempt tp start the http server
// should be made.
func (h *HTTPServerCtrlReconciler) doHTTPStart(logEvent bool) bool {
	return hasCompatibleVersionForHTTPServer(h.VRec, h.Vdb, logEvent, "http server start")
}
