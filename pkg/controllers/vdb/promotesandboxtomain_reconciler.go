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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/promotesandboxtomain"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// PromoteSandboxSubclusterToMainReconciler will  convert local sandbox to main cluster
type PromoteSandboxSubclusterToMainReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts      *PodFacts
	InitiatorIP string // The IP of the pod that we run vclusterOps from
	Dispatcher  vadmin.Dispatcher
}

// MakePromoteSandboxSubclusterToMainReconciler will build a SandboxSubclusterReconciler object
func MakePromoteSandboxSubclusterToMainReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &PromoteSandboxSubclusterToMainReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("promoteSandboxSubclusterToMainReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

// Reconcile will add subclusters to sandboxes if we found any qualified subclusters
func (s *PromoteSandboxSubclusterToMainReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db or no sandboxes
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly ||
		!s.Vdb.IsEON() || len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	sandboxName := s.PFacts.GetSandboxName()
	return s.promoteSandboxToMain(ctx, sandboxName)
}

// sandboxSubclusters will add subclusters to their sandboxes defined in the vdb
func (s *PromoteSandboxSubclusterToMainReconciler) promoteSandboxToMain(ctx context.Context, sandboxName string) (ctrl.Result, error) {
	initiator, ok := s.PFacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.sandbox == vapi.MainCluster && v.isPrimary && v.upNode
	})
	if !ok {
		s.Log.Info("Requeue because there are no UP nodes in main cluster to execute sandbox operation")
		return ctrl.Result{Requeue: true}, nil
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.PromoteSandboxToMainStart,
		"Starting promote sandbox %q to main", sandboxName)

	// find sandbox name
	err := s.Dispatcher.PromoteSandboxToMain(ctx,
		promotesandboxtomain.WithInitiator(initiator.podIP),
		promotesandboxtomain.WithSandbox(sandboxName),
	)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.PromoteSandboxToMainFailed,
			"Failed to promote sandbox %q to main", sandboxName)
		return ctrl.Result{}, err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.PromoteSandboxToSucceeded,
		"Successfully promote sandbox %q to main", sandboxName)
	return ctrl.Result{}, nil
}
