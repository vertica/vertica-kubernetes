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
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsc"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

type StopSubclusterReconciler struct {
	Rec            config.ReconcilerInterface
	Vdb            *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner        cmds.PodRunner
	Log            logr.Logger
	PFacts         *PodFacts
	InitiatorPodIP string
	Dispatcher     vadmin.Dispatcher
}

func MakeStopSubclusterReconciler(rec config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &StopSubclusterReconciler{
		Rec:        rec,
		Log:        log.WithName("StopSubclusterReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

func (s *StopSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := s.PFacts.Collect(ctx, s.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	scsToStop := s.findSubclustersWithShutdownNeeded()
	if len(scsToStop) == 0 {
		s.Log.Info("Aborting stop subcluster. No subclusters needing shutdown were found.")
		return ctrl.Result{}, nil
	}
	oldSize := len(scsToStop)
	scsToStop = s.filterSubclustersWithRunningPods(scsToStop)
	needRequeue := false
	if len(scsToStop) != oldSize {
		needRequeue = true
	}

	if err := s.stopSubclusters(ctx, scsToStop); err != nil {
		return ctrl.Result{}, err
	}

	if needRequeue {
		s.Log.Info("Some subclusters that need shutdown have pods not running. Requeue reconciliation.")
	}
	return ctrl.Result{Requeue: needRequeue}, nil
}

// getFirstUpSCPodIP finds and return the first up node in the given
// subcluster
func (s *StopSubclusterReconciler) getFirstUpSCPodIP(scName string) (string, bool) {
	return s.PFacts.FindFirstUpPodIP(true, scName)
}

// stopSubclusters call the stop subcluster api call on the given subclusters
func (s *StopSubclusterReconciler) stopSubclusters(ctx context.Context, scs []string) error {
	for _, sc := range scs {
		// A subcluster needs to be shutdown only if at least
		// one node is up. So we check for an up node and if we find
		// one, we use it as initiator for stop subcluster, if we don't
		// then we skip the current subcluster and move to next one
		ip, ok := s.getFirstUpSCPodIP(sc)
		if !ok {
			continue
		}
		err := s.stopSubcluster(ctx, sc, ip)
		if err != nil {
			return err
		}
		// Invalidate the cached pod facts now that at least one subcluster was shutdown
		s.PFacts.Invalidate()
	}
	return nil
}

func (s *StopSubclusterReconciler) buildOpts(sc, ip string) []stopsc.Option {
	return []stopsc.Option{
		stopsc.WithInitiator(ip),
		stopsc.WithSubcluster(sc),
	}
}

// stopSubcluster calls the API that will perform stop subcluster
func (s *StopSubclusterReconciler) stopSubcluster(ctx context.Context, scName, ip string) error {
	opts := s.buildOpts(scName, ip)
	// call vcluster API
	s.Rec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterStarted,
		"Starting stop subcluster for %q", scName)
	start := time.Now()
	if err := s.Dispatcher.StopSubcluster(ctx, opts...); err != nil {
		return err
	}
	s.Rec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterSucceeded,
		"Successfully stopped subcluster %q in %s", scName, time.Since(start).Truncate(time.Second))
	return nil
}

// findSubclustersWithShutdownNeeded finds subcluster candidates to a shutdown.
// A subcluster may require a shutdown if it is not defined in a sandbox(not in vdb.spec.sandboxes)
// but is still part of a sandbox(in vdb.status.sandboxes)
func (s *StopSubclusterReconciler) findSubclustersWithShutdownNeeded() []string {
	unsandboxSbScMap := s.Vdb.GenSandboxSubclusterMapForUnsandbox()
	return unsandboxSbScMap[s.PFacts.GetSandboxName()]
}

func (s *StopSubclusterReconciler) filterSubclustersWithRunningPods(scs []string) []string {
	newSCs := []string{}
	scMap := s.Vdb.GenSubclusterMap()
	for _, sc := range scs {
		subcluster := scMap[sc]
		if subcluster == nil {
			// It is very unlikely we reach this part because
			// sandboxed subclusterare always in the vdb spec
			continue
		}
		runningPods := s.PFacts.CountRunningAndInstalled(subcluster.Name)
		if int32(runningPods) == subcluster.Size {
			newSCs = append(newSCs, sc)
		}
	}
	return newSCs
}
