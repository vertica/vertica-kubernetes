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
	"os"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/atconf"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UninstallReconciler will handle reconcile looking specifically for scale down
// events.
type UninstallReconciler struct {
	VRec     *VerticaDBReconciler
	Log      logr.Logger
	Vdb      *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner  cmds.PodRunner
	PFacts   *PodFacts
	ATWriter atconf.Writer
}

// MakeUninstallReconciler will build and return the UninstallReconciler object.
func MakeUninstallReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &UninstallReconciler{
		VRec:     vdbrecon,
		Log:      log,
		Vdb:      vdb,
		PRunner:  prunner,
		PFacts:   pfacts,
		ATWriter: atconf.MakeFileWriter(log, vdb, prunner),
	}
}

func (s *UninstallReconciler) GetClient() client.Client {
	return s.VRec.Client
}

func (s *UninstallReconciler) GetVDB() *vapi.VerticaDB {
	return s.Vdb
}

func (s *UninstallReconciler) CollectPFacts(ctx context.Context) error {
	return s.PFacts.Collect(ctx, s.Vdb)
}

// Reconcile will handle state where a pod in a subcluster is being scaled down.
// During a scale down we need to drive uninstall logic for each applicable pod.
//
// This reconcile function is meant to be called before we create/delete any
// kubernetes objects. It allows us to look at the state before applying
// everything in Vdb. We will know if we are scaling down by comparing the
// expected subcluster size with the current.
func (s *UninstallReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// We can only proceed with install if all of the pods are running.  This
	// ensures we can properly sync admintools.conf.
	if ok, podNotRunning := s.PFacts.anyPodsNotRunning(); ok {
		s.Log.Info("At least one pod isn't running.  Aborting the uninstall.", "pod", podNotRunning)
		return ctrl.Result{Requeue: true}, nil
	}

	// We need to use the finder so that we include subclusters that don't exist
	// in the vdb.  We need to call uninstall for each pod that is part of a
	// deleted subcluster.
	finder := iter.MakeSubclusterFinder(s.VRec.Client, s.Vdb)
	subclusters, err := finder.FindSubclusters(ctx, iter.FindAll)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range subclusters {
		if res, err := s.reconcileSubcluster(ctx, subclusters[i]); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileSubcluster Will handle reconcile for a single subcluster
// This will check for uninstall at a single cluster and handle if uninstall is needed.
func (s *UninstallReconciler) reconcileSubcluster(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	return scaledownSubcluster(ctx, s, sc, s.uninstallPodsInSubcluster)
}

// uninstallPodsInSubcluster will call uninstall on a range of pods that will be scaled down
func (s *UninstallReconciler) uninstallPodsInSubcluster(ctx context.Context, sc *vapi.Subcluster,
	startPodIndex, endPodIndex int32) (ctrl.Result, error) {
	podsToUninstall, requeueNeeded := s.findPodsSuitableForScaleDown(sc, startPodIndex, endPodIndex)
	if len(podsToUninstall) > 0 {
		basePod, err := findATBasePod(s.Vdb, s.PFacts)
		if err != nil {
			return ctrl.Result{}, err
		}
		ipsToUninstall := []string{}
		for _, p := range podsToUninstall {
			ipsToUninstall = append(ipsToUninstall, p.podIP)
		}
		atConfTempFile, err := s.ATWriter.RemoveHosts(ctx, basePod, ipsToUninstall)
		if err != nil {
			return ctrl.Result{}, err
		}
		defer os.Remove(atConfTempFile)

		if err := distributeAdmintoolsConf(ctx, s.Vdb, s.VRec, s.PFacts, s.PRunner, atConfTempFile); err != nil {
			return ctrl.Result{}, err
		}

		// Remove the installer indicator file so that we do an install if we then
		// opt to scale out again.
		cmd := s.genCmdRemoveInstallIndicator()
		for _, pod := range podsToUninstall {
			if _, _, err := s.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to call remove installer indicator file: %w", err)
			}
		}

		// We successfully uninstalled at least one pod, invalidate the pod
		// facts cache so that it is refreshed the next time we read it.
		s.PFacts.Invalidate()
	}

	return ctrl.Result{Requeue: requeueNeeded}, nil
}

// findPodsSuitableForScaleDown will return a list of host names that can be uninstalled
// If a pod was skipped that may require an uninstall, then the bool return
// comes back as true. It is the callers responsibility to requeue a
// reconciliation if that is true.
func (s *UninstallReconciler) findPodsSuitableForScaleDown(sc *vapi.Subcluster, startPodIndex, endPodIndex int32) ([]*PodFact, bool) {
	pods := []*PodFact{}
	requeueNeeded := false
	for podIndex := startPodIndex; podIndex <= endPodIndex; podIndex++ {
		uninstallPod := names.GenPodName(s.Vdb, sc, podIndex)
		podFact, ok := s.PFacts.Detail[uninstallPod]
		if !ok || !podFact.isPodRunning {
			// Requeue since we need the pod running to remove the installer indicator file
			s.Log.Info("Pod may require uninstall but not able to do so now", "pod", uninstallPod)
			requeueNeeded = true
			continue
		}
		// Fine to skip if installer hasn't even been run for this pod
		if podFact.isInstalled.IsFalse() {
			continue
		}
		if !podFact.dbExists.IsFalse() {
			s.Log.Info("DB exists at the pod, which needs to be removed first", "pod", uninstallPod)
			requeueNeeded = true
			continue
		}
		pods = append(pods, podFact)
	}
	return pods, requeueNeeded
}

// genCmdRemoveInstallIndicator will generate the command to get rid of the installer indicator file
func (s *UninstallReconciler) genCmdRemoveInstallIndicator() []string {
	return []string{"rm", s.Vdb.GenInstallerIndicatorFileName()}
}
