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
	"time"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
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

	timeout := vmeta.GetRemoveDrainSeconds(s.Vdb.Annotations)
	if timeout == vmeta.RemoveDrainSecondsDisabledValue {
		return s.reconcilePods(ctx)
	}
	hasTimeoutZero := false
	if timeout == 0 {
		timeout = 1
		hasTimeoutZero = true
	}

	// Note: this reconciler depends on the client routing reconciler to have run
	// and directed traffic away from pending delete pods.
	pfs := []*podfacts.PodFact{}
	for i := 0; i < timeout; i++ {
		active := false
		for _, pf := range s.PFacts.Detail {
			if pf.GetIsPendingDelete() && pf.GetUpNode() {
				result, err := s.reconcilePod(ctx, pf)
				if err != nil {
					return ctrl.Result{}, err
				} else if result.Requeue {
					active = true
					// we start collecting pods that needs connections to be killed in
					// the last second
					if i+1 == timeout {
						pfs = append(pfs, pf)
					}
				}
			}
		}
		if !active {
			return ctrl.Result{}, nil
		}
		if hasTimeoutZero {
			break
		}
		time.Sleep(1 * time.Second)
	}

	return ctrl.Result{}, s.waitForConnectionsToEnd(ctx, pfs)
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

func (s *DrainNodeReconciler) waitForConnectionsToEnd(ctx context.Context, pfs []*podfacts.PodFact) error {
	if len(pfs) == 0 {
		return nil
	}
	sessionIds := []string{}
	for _, pf := range pfs {
		sql := fmt.Sprintf(
			"select session_id"+
				" from sessions"+
				" where node_name = '%s'"+
				" and session_id not in ("+
				" select session_id from current_session"+
				" )", pf.GetVnodeName())
		stdout, stderr, err := s.PFacts.PRunner.ExecVSQL(ctx, pf.GetName(), names.ServerContainer, "-tAc", sql)
		if err != nil {
			s.VRec.Log.Error(err, "failed to retrieve active sessions", "stderr", stderr)
			return err
		}
		sessionIds = append(sessionIds, strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")...)
	}

	return killSessions(ctx, sessionIds, s.PFacts, pfs[0], s.VRec.Log)
}

func (s *DrainNodeReconciler) reconcilePods(ctx context.Context) (ctrl.Result, error) {
	for _, pf := range s.PFacts.Detail {
		if pf.GetIsPendingDelete() && pf.GetUpNode() {
			if res, err := s.reconcilePod(ctx, pf); verrors.IsReconcileAborted(res, err) {
				return res, err
			}
		}
	}
	return ctrl.Result{}, nil
}
