/*
 (c) Copyright [2021-2022] Micro Focus or one of its affiliates.
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
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type DrainNodeReconciler struct {
	VRec    *VerticaDBReconciler
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

func MakeDrainNodeReconciler(vdbrecon *VerticaDBReconciler,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
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
func (s *DrainNodeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Note: this reconciler depends on the clien routing reconciler to have run
	// and directed traffic away from pending delete pods.
	for _, pf := range s.PFacts.Detail {
		if pf.pendingDelete && pf.upNode {
			if res, err := s.reconcilePod(ctx, pf); verrors.IsReconcileAborted(res, err) {
				return res, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// reconcilePod will handle drain logic for a single pod
func (s *DrainNodeReconciler) reconcilePod(ctx context.Context, pf *PodFact) (ctrl.Result, error) {
	sql := fmt.Sprintf(
		"select count(*)"+
			" from sessions"+
			" where node_name = '%s'"+
			" and session_id not in ("+
			" select session_id from current_session"+
			" )", pf.vnodeName)
	cmd := []string{"-tAc", sql}
	stdout, _, err := s.PRunner.ExecVSQL(ctx, pf.name, names.ServerContainer, cmd...)
	if err != nil {
		return ctrl.Result{}, err
	}
	// If there is an active connection, we will requeue, which causes us to use
	// the exponential backoff algorithm.
	activeConnections := anyActiveConnections(stdout)
	if activeConnections {
		s.VRec.EVRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.DrainNodeRetry,
			"Pod '%s' has active connections preventing the drain from succeeding", pf.name.Name)
	}
	return ctrl.Result{Requeue: activeConnections}, nil
}
