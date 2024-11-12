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
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/stopsubcluster"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// SubclusterShutdownReconciler will handle the process when subclusters
// needs to be shut down or restart
type SubclusterShutdownReconciler struct {
	VRec       config.ReconcilerInterface
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
}

// MakeSubclusterShutdownReconciler will build a SubclusterShutdownReconciler object
func MakeSubclusterShutdownReconciler(recon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, dispatcher vadmin.Dispatcher, pfacts *podfacts.PodFacts) controllers.ReconcileActor {
	return &SubclusterShutdownReconciler{
		VRec:       recon,
		Log:        log.WithName("SubclusterShutdownReconciler"),
		Vdb:        vdb,
		Dispatcher: dispatcher,
		PFacts:     pfacts,
	}
}

func (s *SubclusterShutdownReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	subclusters, err := s.getSubclustersToShutdown()
	if err != nil {
		return ctrl.Result{}, err
	}
	for scName, initIP := range subclusters {
		err := s.PFacts.RemoveStartupFileInSubclusterPods(ctx, scName, "removed startup.json before stop_subcluster")
		if err != nil {
			return ctrl.Result{}, err
		}
		err = s.runStopSubclusterVclusterAPI(ctx, scName, initIP)
		if err != nil {
			return ctrl.Result{}, err
		}
		s.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}

// getSubclustersToShutdown returns the subclusters that need to get
// shut down
func (s *SubclusterShutdownReconciler) getSubclustersToShutdown() (map[string]string, error) {
	subclusters := map[string]string{}
	primarySubclusters := []string{}
	upPrimaryNodes := 0
	willLoseQuorum := false
	scSbMap := s.Vdb.GenSubclusterSandboxMap()
	sbMap := s.Vdb.GenSandboxMap()
	for i := range s.Vdb.Spec.Subclusters {
		sc := &s.Vdb.Spec.Subclusters[i]
		sandbox := scSbMap[sc.Name]
		if sandbox != s.PFacts.GetSandboxName() {
			continue
		}
		// no-op if the subcluster is not marked for
		// shutdown
		if !sc.Shutdown {
			continue
		}
		if sandbox != vapi.MainCluster {
			sb := sbMap[sandbox]
			// no-op if the subcluster shutdown is driven
			// by its sandbox
			if sb != nil && sb.Shutdown {
				continue
			}
		}
		hostIP, ok := s.PFacts.FindFirstUpPodIP(false, sc.Name)
		if !ok {
			continue
		}
		if sc.IsPrimary() {
			primarySubclusters = append(primarySubclusters, sc.Name)
			upPrimaryNodes += s.PFacts.GetSubclusterUpNodeCount(sc.Name)
			// If stopping a subcluster would cause cluster quorum, we abort
			// the operation
			if !s.PFacts.DoesDBHaveQuorum(upPrimaryNodes) {
				willLoseQuorum = true
				break
			}
		}
		subclusters[sc.Name] = hostIP
	}
	if willLoseQuorum {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.ClusterWillLoseQuorum,
			"Shutting down subclusters %s will cause quorum loss.", strings.Join(primarySubclusters, ","))
		return subclusters, fmt.Errorf("cannot shut down primaries %s because it will cause quorum, loss. "+
			"please revert back", strings.Join(primarySubclusters, ","))
	}
	return subclusters, nil
}

// runStopSubclusterVclusterAPI will do the actual execution of stop subcluster.
// This handles logging of necessary events.
func (s *SubclusterShutdownReconciler) runStopSubclusterVclusterAPI(ctx context.Context, scName, host string) error {
	opts := s.genStopSubclusterOpts(host, scName)
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterStart, "Starting stop subcluster %q.",
		scName)

	err := s.Dispatcher.StopSubcluster(ctx, opts...)
	if err != nil {
		// For all other errors, return error
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.StopSubclusterFailed,
			"Failed to stop subcluster %q", scName)
		return err
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.StopSubclusterSucceeded,
		"Successfully stopped subcluster %q.", scName)
	return nil
}

// genStopSubclusterOpts will return the options to use with the stop subcluster api
func (s *SubclusterShutdownReconciler) genStopSubclusterOpts(initiatorIP, scName string) []stopsubcluster.Option {
	opts := []stopsubcluster.Option{
		stopsubcluster.WithInitiator(initiatorIP),
		stopsubcluster.WithSCName(scName),
		stopsubcluster.WithDrainSeconds(s.Vdb.GetStopSCDrainSeconds()),
	}
	return opts
}
