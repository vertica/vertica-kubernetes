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
	"time"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopdb"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StopDBReconciler will stop the cluster and clear the restart needed status condition
type StopDBReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner    cmds.PodRunner
	PFacts     *PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeStopDBReconciler will build a StopDBReconciler object
func MakeStopDBReconciler(
	vdbrecon *VerticaDBReconciler, vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	dispatcher vadmin.Dispatcher,
) controllers.ReconcileActor {
	return &StopDBReconciler{
		VRec:       vdbrecon,
		Vdb:        vdb,
		PRunner:    prunner,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

// Reconcile will stop vertica if the status condition indicates a restart is needed
func (s *StopDBReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := s.PFacts.Collect(ctx, s.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if no database exists
	if !s.PFacts.doesDBExist() {
		return ctrl.Result{}, nil
	}

	// Only proceed if the restart needed status condition is set.
	isSet := s.Vdb.IsStatusConditionTrue(vapi.VerticaRestartNeeded)
	if isSet {
		// Stop vertica if any pods are running
		if s.PFacts.getUpNodeCount() > 0 {
			err = s.stopVertica(ctx)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		// Clear the condition now that we stopped the cluster.  We rely on the
		// restart reconciler that follows this to bring up vertica.
		err = vdbstatus.UpdateCondition(ctx, s.VRec.Client, s.Vdb,
			vapi.MakeCondition(vapi.VerticaRestartNeeded, metav1.ConditionFalse, "StopCompleted"),
		)
	}
	return ctrl.Result{}, err
}

// stopVertica will stop vertica on all of the running pods
func (s *StopDBReconciler) stopVertica(ctx context.Context) error {
	pf, ok := s.PFacts.findPodToRunAdminCmdAny()
	if !ok {
		// If no running pod found, then there is nothing to stop and we can just continue on
		return nil
	}

	// Run the stop_db command
	err := s.runATCmd(ctx, pf.name, pf.podIP)

	// Invalidate the pod facts now that vertica daemon has been stopped on all of the pods
	s.PFacts.Invalidate()
	return err
}

// runATCmd issues the admintools command to stop the database
func (s *StopDBReconciler) runATCmd(ctx context.Context, initiatorName types.NamespacedName, initiatorIP string) error {
	s.VRec.Event(s.Vdb, corev1.EventTypeNormal, events.StopDBStart, "Starting stop database")
	start := time.Now()
	if err := s.Dispatcher.StopDB(ctx, stopdb.WithInitiator(initiatorName, initiatorIP)); err != nil {
		s.VRec.Event(s.Vdb, corev1.EventTypeWarning, events.StopDBFailed, "Failed to stop the database")
		return err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopDBSucceeded,
		"Successfully stopped the database.  It took %ds", int(time.Since(start).Seconds()))
	return nil
}
