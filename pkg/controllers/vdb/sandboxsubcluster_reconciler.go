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
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/sandboxsc"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SandboxSubclusterReconciler will add subclusters to sandboxes
type SandboxSubclusterReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts      *PodFacts
	InitiatorIP string // The IP of the pod that we run vclusterOps from
	Dispatcher  vadmin.Dispatcher
	client.Client
}

// MakeSandboxSubclusterReconciler will build a SandboxSubclusterReconciler object
func MakeSandboxSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client) controllers.ReconcileActor {
	return &SandboxSubclusterReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("SandboxSubclusterReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
		Client:     cli,
	}
}

// Reconcile will add subclusters to sandboxes if we found any qualified subclusters
func (s *SandboxSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db or no sandboxes
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly ||
		!s.Vdb.IsEON() || len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return s.sandboxSubclusters(ctx)
}

// sandboxSubclusters will add subclusters to their sandboxes defined in the vdb
func (s *SandboxSubclusterReconciler) sandboxSubclusters(ctx context.Context) (ctrl.Result, error) {
	// find qualified subclusters with their sandboxes
	scSbMap, err := s.fetchSubclustersWithSandboxes()
	// the error indicates we have down nodes in a subcluster that will be sandboxed.
	// we simply requeue the iteration for waiting the nodes to be up.
	if err != nil {
		s.Log.Info("Requeue because a pod is not ready", "details", err.Error())
		return ctrl.Result{}, nil
	}
	if len(scSbMap) == 0 {
		s.Log.Info("No subclusters need to be sandboxed")
		return ctrl.Result{}, nil
	}

	// find an initiator to call vclusterOps
	initiator, ok := s.PFacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.sandbox == "" && v.isPrimary && v.upNode
	})
	if ok {
		s.InitiatorIP = initiator.podIP
	} else {
		s.Log.Info("Requeue because there are no UP nodes in main cluster to execute sandbox operation")
		return ctrl.Result{}, nil
	}

	err = s.executeSandboxCommand(ctx, scSbMap)
	if err != nil {
		return ctrl.Result{}, err
	}
	s.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// executeSandboxCommand will call sandbox API in vclusterOps, then update sandbox status in vdb
func (s *SandboxSubclusterReconciler) executeSandboxCommand(ctx context.Context, scSbMap map[string]string) error {
	succeedSbScMap := make(map[string][]string)
	for sc, sb := range scSbMap {
		err := s.sandboxSubcluster(ctx, sc, sb)
		if err != nil {
			// when one subcluster failed to be sandboxed, update sandbox status and return error
			return errors.Join(err, s.updateSandboxStatus(ctx, succeedSbScMap))
		} else {
			succeedSbScMap[sb] = append(succeedSbScMap[sb], sc)
		}
	}
	return s.updateSandboxStatus(ctx, succeedSbScMap)
}

// fetchSubclustersWithSandboxes will return the qualified subclusters with their sandboxes
func (s *SandboxSubclusterReconciler) fetchSubclustersWithSandboxes() (map[string]string, error) {
	vdbScSbMap := s.Vdb.GenSubclusterSandboxMap()
	targetScSbMap := make(map[string]string)
	for _, v := range s.PFacts.Detail {
		sb, ok := vdbScSbMap[v.subclusterName]
		// skip the pod in the subcluster that doesn't need to be sandboxed
		if !ok {
			continue
		}
		// skip the pod in the subcluster that already in the target sandbox
		if sb == v.sandbox {
			continue
		}
		// the pod to be added in a sandbox should have a running node
		if !v.upNode {
			return targetScSbMap, fmt.Errorf("pod %q does not contain an UP Vertica node so it cannot be added to sandbox %q yet",
				v.name.Name, sb)
		}
		targetScSbMap[v.subclusterName] = sb
	}
	return targetScSbMap, nil
}

// sandboxSubcluster will add a subcluster to a sandbox by calling vclusterOps
func (s *SandboxSubclusterReconciler) sandboxSubcluster(ctx context.Context, subcluster, sandbox string) error {
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SandboxSubclusterStart,
		"Starting add subcluster %q to sandbox %q", subcluster, sandbox)
	err := s.Dispatcher.SandboxSubcluster(ctx,
		sandboxsc.WithInitiator(s.InitiatorIP),
		sandboxsc.WithSubcluster(subcluster),
		sandboxsc.WithSandbox(sandbox),
	)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.SandboxSubclusterFailed,
			"Failed to add subcluster %q to sandbox %q", subcluster, sandbox)
		return err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SandboxSubclusterSucceeded,
		"Successfully added subcluster %q to sandbox %q", subcluster, sandbox)
	return nil
}

// updateSandboxStatus will update sandbox status in vdb
func (s *SandboxSubclusterReconciler) updateSandboxStatus(ctx context.Context, originalSbScMap map[string][]string) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// make a copy of originalSbScMap since we will modify the map, and
		// we want to have a good map in next retry
		sbScMap := make(map[string][]string, len(originalSbScMap))
		for sb, scs := range originalSbScMap {
			sbScMap[sb] = make([]string, len(scs))
			copy(sbScMap[sb], scs)
		}

		// for existing sandboxes, update their subclusters in sandbox status
		for i, sbStatus := range vdbChg.Status.Sandboxes {
			scs, ok := sbScMap[sbStatus.Name]
			if ok {
				vdbChg.Status.Sandboxes[i].Subclusters = append(vdbChg.Status.Sandboxes[i].Subclusters, scs...)
				delete(sbScMap, sbStatus.Name)
			}
		}

		// for new sandboxes, append them in sandbox status
		for sb, scs := range sbScMap {
			newStatus := vapi.SandboxStatus{Name: sb, Subclusters: scs}
			vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes, newStatus)
		}
		return nil
	}

	return vdbstatus.Update(ctx, s.Client, s.Vdb, updateStatus)
}
