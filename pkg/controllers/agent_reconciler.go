/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AgentReconciler will ensure the agent is running
type AgentReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeAgentReconciler will build a AgentReonciler object
func MakeAgentReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &AgentReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will ensure the agent is running and start it if it isn't
func (a *AgentReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	for _, pod := range a.PFacts.Detail {
		if pod.agentRunning {
			continue
		}
		// Only start the agent for pods that have been added to a database
		if !pod.dbExists.IsTrue() {
			continue
		}
		// We don't start the vertica agent if running ipv6 since the agent
		// currently doesn't work in that mode.  This can be removed once
		// VER-77406 is addressed.
		if isIPv6(pod.podIP) {
			continue
		}
		if err := a.startAgentInPod(ctx, pod); err != nil {
			return ctrl.Result{}, err
		}

		// Invalidate the pod facts cache since its out of date due to the agent starting
		a.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}

// startAgentInPod will start the agent in the given pod.
func (a *AgentReconciler) startAgentInPod(ctx context.Context, pod *PodFact) error {
	cmd := []string{
		"sudo", "/opt/vertica/sbin/vertica_agent", "start",
	}
	if _, _, err := a.PRunner.ExecInPod(ctx, pod.name, ServerContainer, cmd...); err != nil {
		return err
	}
	return nil
}
