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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RebalanceShardsReconciler will ensure each node has at least one shard subscription
type RebalanceShardsReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	ScName  string // Name of the subcluster to rebalance.  Leave this blank if you want to handle all subclusters.
}

// MakeRebalanceShardsReconciler will build a RebalanceShardsReconciler object
func MakeRebalanceShardsReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts, scName string) controllers.ReconcileActor {
	return &RebalanceShardsReconciler{
		VRec:    vdbrecon,
		Log:     log,
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
		ScName:  scName,
	}
}

// Reconcile will ensure each node has at least one shard subscription
func (s *RebalanceShardsReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	scToRebalance := s.findShardsToRebalance()
	if len(scToRebalance) == 0 {
		return ctrl.Result{}, nil
	}

	atPod, ok := s.PFacts.findPodToRunVsql(false, "")
	if !ok {
		s.Log.Info("No pod found to run vsql from. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range scToRebalance {
		if err := s.rebalanceShards(ctx, atPod, scToRebalance[i]); err != nil {
			return ctrl.Result{}, err
		}
		s.PFacts.Invalidate() // Refresh due to shard subscriptions have changed
	}

	return ctrl.Result{}, nil
}

// findShardsToRebalance will populate the scToRebalance slice with subclusters
// that need a rebalance
func (s *RebalanceShardsReconciler) findShardsToRebalance() []string {
	scRebalanceMap := map[string]bool{}
	scToRebalance := []string{}

	for _, pf := range s.PFacts.Detail {
		if (s.ScName == "" || s.ScName == pf.subcluster) && pf.isPodRunning && pf.upNode && pf.shardSubscriptions == 0 {
			_, ok := scRebalanceMap[pf.subcluster]
			if !ok {
				scToRebalance = append(scToRebalance, pf.subcluster)
				scRebalanceMap[pf.subcluster] = true
			}
		}
	}
	return scToRebalance
}

// rebalanceShards will run rebalance_shards for the given subcluster
func (s *RebalanceShardsReconciler) rebalanceShards(ctx context.Context, atPod *PodFact, scName string) error {
	podName := atPod.name
	selectCmd := fmt.Sprintf("select rebalance_shards('%s')", scName)
	cmd := []string{
		"-tAc", selectCmd,
	}
	_, _, err := s.PRunner.ExecVSQL(ctx, podName, names.ServerContainer, cmd...)
	if err != nil {
		return err
	}
	s.VRec.EVRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.RebalanceShards,
		"Successfully called 'rebalance_shards' for '%s'", scName)

	return nil
}
