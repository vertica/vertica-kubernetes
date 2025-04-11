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
	"time"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DrainNodeReconciler struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *podfacts.PodFacts
}

func MakeDrainNodeReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &DrainNodeReconciler{
		VRec:    vdbrecon,
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
	}
}

// Reconcile will wait for active connections to leave in any pod that is marked
// as pending delete.  This will drain those pods that we are going to scale
// down before we actually remove them from the cluster.
func (s *DrainNodeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Note: this reconciler depends on the client routing reconciler to have run
	// and directed traffic away from pending delete pods.
	timeoutInt := s.Vdb.GetActiveConnectionsDrainSeconds()
	// If timeout is zero, we move on to the next reconciler (DBRemoveNodeReconciler|DBRemoveSubclusterReconciler)
	if timeoutInt == 0 {
		return ctrl.Result{}, nil
	}

	pfs, err := s.getPendingDeletePods(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If no pending delete pods have active connection, we remove
	// the drain start time annotation (if set) and return
	if len(pfs) == 0 {
		return ctrl.Result{}, s.removeDrainStartAnnotation(ctx)
	}

	drainStartTimeStr, found := s.Vdb.Annotations[vmeta.DrainStartAnnotation]
	// If drain start time annotation is not set, we set it and requeue after 1s
	if !found {
		s.VRec.Log.Info("Starting draining before removing nodes")
		return ctrl.Result{RequeueAfter: 1 * time.Second}, s.setDrainStartAnnotation(ctx)
	}

	var drainStartTime time.Time
	drainStartTime, err = time.Parse(time.RFC3339, drainStartTimeStr)
	if err != nil {
		return ctrl.Result{}, err
	}
	elapsed := time.Since(drainStartTime)
	timeout := time.Duration(timeoutInt) * time.Second
	// If timeout has expired, we move on to the next reconciler (DBRemoveNodeReconciler|DBRemoveSubclusterReconciler)
	if elapsed >= timeout {
		s.VRec.Log.Info("Draining timeout has expired")
		return ctrl.Result{}, s.removeDrainStartAnnotation(ctx)
	}

	return ctrl.Result{RequeueAfter: 1 * time.Second}, nil
}

// reconcilePod will handle drain logic for a single pod
func (s *DrainNodeReconciler) reconcilePod(ctx context.Context, pf *podfacts.PodFact) (ctrl.Result, error) {
	sql := fmt.Sprintf(
		"select count(*)"+
			" from sessions"+
			" where node_name = '%s'"+
			" and session_id not in ("+
			" select session_id from current_session"+
			" )", pf.GetVnodeName())
	cmd := []string{"-tAc", sql}
	stdout, _, err := s.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, cmd...)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If there is an active connection, we will requeue, which causes us to use
	// the exponential backoff algorithm.
	activeConnections := anyActiveConnections(stdout)
	if activeConnections {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.DrainNodeRetry,
			"Pod '%s' has active connections preventing the drain from succeeding", pf.GetName().Name)
	}
	return ctrl.Result{Requeue: activeConnections}, nil
}

// getPendingDeletePods returns pods that are pending delete and still have active connections
func (s *DrainNodeReconciler) getPendingDeletePods(ctx context.Context) ([]*podfacts.PodFact, error) {
	pfs := []*podfacts.PodFact{}
	for _, pf := range s.PFacts.FindPendingDeletePods() {
		result, err := s.reconcilePod(ctx, pf)
		if err != nil {
			return nil, err
		} else if result.Requeue {
			pfs = append(pfs, pf)
		}
	}
	return pfs, nil
}

func (s *DrainNodeReconciler) removeDrainStartAnnotation(ctx context.Context) error {
	clearDrainStartAnnotation := func() (updated bool, err error) {
		if _, found := s.Vdb.Annotations[vmeta.DrainStartAnnotation]; found {
			delete(s.Vdb.Annotations, vmeta.DrainStartAnnotation)
			updated = true
		}
		return
	}
	_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, clearDrainStartAnnotation)
	return err
}

func (s *DrainNodeReconciler) setDrainStartAnnotation(ctx context.Context) error {
	addDrainStartAnnotation := func() (updated bool, err error) {
		if s.Vdb.Annotations == nil {
			s.Vdb.Annotations = make(map[string]string)
		}
		if _, found := s.Vdb.Annotations[vmeta.DrainStartAnnotation]; !found {
			s.Vdb.Annotations[vmeta.DrainStartAnnotation] = time.Now().Format(time.RFC3339)
			updated = true
		}
		return
	}
	_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, addDrainStartAnnotation)
	return err
}
