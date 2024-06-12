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
	vutil "github.com/vertica/vcluster/vclusterops/util"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/promotesandboxtomain"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// PromoteSandboxToMainReconciler will convert local sandbox to main cluster
type PromoteSandboxToMainReconciler struct {
	VRec        *VerticaDBReconciler
	Log         logr.Logger
	Vdb         *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts      *PodFacts
	InitiatorIP string // The IP of the pod that we run vclusterOps from
	Dispatcher  vadmin.Dispatcher
	client.Client
}

// MakePromoteSandboxToMainReconciler will build a promoteSandboxToMainReconciler object
func MakePromoteSandboxToMainReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client) controllers.ReconcileActor {
	return &PromoteSandboxToMainReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("promoteSandboxToMainReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
		Client:     cli,
	}
}

func (s *PromoteSandboxToMainReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	var res ctrl.Result

	// no-op for ScheduleOnly init policy or enterprise db or no sandboxes
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly ||
		!s.Vdb.IsEON() || len(s.Vdb.Spec.Sandboxes) == 0 {
		return res, nil
	}

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return res, nil
	}

	sandboxName := s.PFacts.GetSandboxName()

	res, err := s.promoteSandboxToMain(ctx, sandboxName)
	if err != nil {
		return res, err
	}

	s.PFacts.Invalidate()

	err = s.updateSandboxScTypeInVdb(ctx, sandboxName)
	if err != nil {
		return res, err
	}
	return res, nil
}

// promoteSandboxToMain call the vclusterOps API to convert local sandbox to main cluster
func (s *PromoteSandboxToMainReconciler) promoteSandboxToMain(ctx context.Context, sandboxName string) (ctrl.Result, error) {
	initiator, ok := s.PFacts.findFirstPodSorted(func(v *PodFact) bool {
		return v.isPrimary && v.upNode
	})
	if !ok {
		s.Log.Info("No Up nodes found. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.PromoteSandboxToMainStart,
		"Starting promote sandbox %q to main", sandboxName)

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

// updateSandboxScTypeInVdb update SandboxPrimarySubcluster to PrimarySubcluster in vdb.spec.subclusters
// and remove sandbox spec and status in vdb after promoting to main
func (s *PromoteSandboxToMainReconciler) updateSandboxScTypeInVdb(ctx context.Context, sandboxName string) error {
	scSbMap := s.Vdb.GenSubclusterSandboxMap()

	// remove sandbox in spec
	for i := len(s.Vdb.Spec.Sandboxes) - 1; i >= 0; i-- {
		_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, func() (bool, error) {
			if s.Vdb.Spec.Sandboxes[i].Name == sandboxName {
				s.Vdb.Spec.Sandboxes[i].Subclusters = []vapi.SubclusterName{}
				s.Vdb.Spec.Sandboxes = append(s.Vdb.Spec.Sandboxes[:i], s.Vdb.Spec.Sandboxes[i+1:]...)
			}
			return true, nil
		})
		if err != nil {
			return err
		}
	}

	// update sandboxPrimarySubcluster to primarySubcluster in spec
	for sc, sb := range scSbMap {
		if sb == sandboxName {
			_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, func() (bool, error) {
				for j := range s.Vdb.Spec.Subclusters {
					if s.Vdb.Spec.Subclusters[j].Name == sc && s.Vdb.Spec.Subclusters[j].Type == vapi.SandboxPrimarySubcluster {
						s.Vdb.Spec.Subclusters[j].Type = vapi.PrimarySubcluster
					}
				}
				return true, nil
			})
			if err != nil {
				return err
			}
		}
	}

	// remove sandbox in status
	unsandboxSbScMap := s.Vdb.GenSandboxSubclusterMapForUnsandbox()
	unsandboxedScNames, found := unsandboxSbScMap[sandboxName]
	if !found {
		s.Log.Info("the sandbox inside it does not need to be removed")
		return nil
	}
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// update the sandbox's subclusters in sandbox status
		for i := len(vdbChg.Status.Sandboxes) - 1; i >= 0; i-- {
			if vdbChg.Status.Sandboxes[i].Name != sandboxName {
				continue
			}
			vdbChg.Status.Sandboxes[i].Subclusters = vutil.SliceDiff(vdbChg.Status.Sandboxes[i].Subclusters, unsandboxedScNames)
			if len(vdbChg.Status.Sandboxes[i].Subclusters) == 0 {
				vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes[:i], vdbChg.Status.Sandboxes[i+1:]...)
			}
			break
		}
		return nil
	}
	return vdbstatus.Update(ctx, s.Client, s.Vdb, updateStatus)
}
