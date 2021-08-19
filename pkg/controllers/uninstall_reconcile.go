/*
 (c) Copyright [2021] Micro Focus or one of its affiliates.
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

package controllers

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UninstallReconciler will handle reconcile looking specifically for scale down
// events.
type UninstallReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	ExecPod types.NamespacedName // Pod to execute uninstall from.
}

// MakeUninstallReconciler will build and return the UninstallReconciler object.
func MakeUninstallReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &UninstallReconciler{
		VRec:    vdbrecon,
		Log:     log,
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
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
	// We need to use the finder so that we include subclusters that don't exist
	// in the vdb.  We need to call uninstall for each pod that is part of a
	// deleted subcluster.
	finder := MakeSubclusterFinder(s.VRec.Client, s.Vdb)
	subclusters, err := finder.FindSubclusters(ctx, FindAll)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range subclusters {
		if res, err := s.reconcileSubcluster(ctx, subclusters[i]); err != nil || res.Requeue {
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
		cmd := genCmdUninstall(podsToUninstall)
		execPod := s.findPodToUninstallFrom()
		if execPod == nil {
			// Requeue since we couldn't find a running pod
			return ctrl.Result{Requeue: true}, nil
		}
		if res, err := s.execATCmd(ctx, *execPod, podsToUninstall, cmd); err != nil || res.Requeue {
			return res, err
		}
		// Remove the installer indicator file so that we do an install if we then
		// opt to scale out again.
		cmd = s.genCmdRemoveInstallIndicator()
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

// execATCmd will execute the admintools command and handle event logging
func (s *UninstallReconciler) execATCmd(ctx context.Context, atPod types.NamespacedName, pods []*PodFact,
	cmd []string) (ctrl.Result, error) {
	s.VRec.EVRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.UninstallPods,
		"Calling update_vertica to remove hosts for the following pods: %s", genPodNames(pods))
	start := time.Now()
	stdout, _, err := s.PRunner.ExecInPod(ctx, atPod, names.ServerContainer, cmd...)
	if err != nil {
		r := regexp.MustCompile(`Unable to remove host\(s\) \[(.*)\]: not part of the cluster`)
		m := r.FindStringSubmatch(stdout)
		if m != nil {
			s.VRec.EVRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.UninstallHostsMissing,
				"Failed while calling update_vertica because hosts were missing: %s", m[1])
			if err2 := s.repairMissingHosts(ctx, m[1]); err2 != nil {
				return ctrl.Result{}, err2
			}
			// Requeue to try the uninstall again
			return ctrl.Result{Requeue: true}, nil
		}
		s.VRec.EVRec.Event(s.Vdb, corev1.EventTypeWarning, events.UninstallFailed,
			"Failed while calling update_vertica")
		return ctrl.Result{}, err
	}
	s.VRec.EVRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.UninstallSucceeded,
		"Successfully called update_vertica to remove hosts and it took %s", time.Since(start))
	return ctrl.Result{}, nil
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
		pods = append(pods, podFact)
	}
	return pods, requeueNeeded
}

// genCmdUninstall generates the command to use to uninstall a single host
func genCmdUninstall(uninstallPods []*PodFact) []string {
	hostNames := []string{}
	for _, p := range uninstallPods {
		hostNames = append(hostNames, p.dnsName)
		// we created hosts with either ipv4 or ipv6
		// if any pod with ipv6, we will remove them with the ipv6 flag
	}

	updateVerticaCmd := []string{
		"sudo", "/opt/vertica/sbin/update_vertica",
		"--remove-hosts", strings.Join(hostNames, ","),
		"--no-package-checks",
	}
	if podsAllHaveIPv6(uninstallPods) {
		return append(updateVerticaCmd, "--ipv6")
	}
	return updateVerticaCmd
}

// genCmdRemoveInstallIndicator will generate the command to get rid of the installer indicator file
func (s *UninstallReconciler) genCmdRemoveInstallIndicator() []string {
	return []string{"rm", paths.GenInstallerIndicatorFileName(s.Vdb)}
}

// findPodToUninstallFrom will find a suitable pod to run the install from.
// If no pod is found nil is returned.
func (s *UninstallReconciler) findPodToUninstallFrom() *types.NamespacedName {
	if s.ExecPod == (types.NamespacedName{}) {
		for uninstallPod, pod := range s.PFacts.Detail {
			if pod.isPodRunning {
				s.ExecPod = uninstallPod
				break
			}
		}
	}
	if s.ExecPod == (types.NamespacedName{}) {
		return nil
	}
	return &s.ExecPod
}

// repairMissingHosts if we come across hosts that have already been removed, we
// are going to remove the install indicator file for each.  This way on the
// next iteration, it will not consider removing them.
func (s *UninstallReconciler) repairMissingHosts(ctx context.Context, missingHosts string) error {
	// The existing hosts comes in the form: '10.244.1.120'.  We need to find
	// the pod that corresponds to this IP and remove the install indicator file.
	cmd := s.genCmdRemoveInstallIndicator()
	for _, quotedHost := range strings.Split(missingHosts, " ") {
		host := strings.Trim(quotedHost, "'") // Remove ' that surrounds it
		for _, pod := range s.PFacts.Detail {
			if pod.podIP == host {
				if _, _, err := s.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
