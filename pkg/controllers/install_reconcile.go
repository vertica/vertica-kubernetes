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
	"github.com/vertica/vertica-kubernetes/pkg/license"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InstallReconciler will handle reconcile for install of vertica
type InstallReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeInstallReconciler will build and return the InstallReconciler object.
func MakeInstallReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &InstallReconciler{
		VRec:    vdbrecon,
		Log:     log,
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
	}
}

// Reconcile will ensure Vertica is installed and running in the pods.
func (d *InstallReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// The reconcile loop works by collecting all of the facts about the running
	// pods. We then analyze those facts to determine a course of action to take.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	return d.analyzeFacts(ctx)
}

// analyzeFacts will look at the collected facts and determine the course of action
func (d *InstallReconciler) analyzeFacts(ctx context.Context) (ctrl.Result, error) {
	if res, err := d.runUpdateVerticaAddHosts(ctx); err != nil || res.Requeue {
		return res, err
	}

	if d.anyPodsNotRunning() {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// runUpdateVerticaAddHosts will call 'update_vertica --add-hosts' for any pods that have not yet bootstrapped the config.
func (d *InstallReconciler) runUpdateVerticaAddHosts(ctx context.Context) (ctrl.Result, error) {
	pods, err := d.getInstallTargets(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(pods) == 0 {
		return ctrl.Result{}, nil
	}

	licensePath, err := license.GetPath(ctx, d.VRec.Client, d.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	pod := d.findPodToInstallFrom()
	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, pods)

	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.InstallingPods,
		"Calling update_vertica to add the following pods as new hosts: %s", genPodNames(pods))
	start := time.Now()
	cmd := d.genCmdInstall(pods, licensePath)
	if stdout, _, err := d.PRunner.ExecInPod(ctx, pod, ServerContainer, cmd...); err != nil {
		r := regexp.MustCompile(`Unable to add host\(s\) \[(.*)\]: already part of the cluster`)
		m := r.FindStringSubmatch(stdout)
		if m != nil {
			d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeWarning, events.InstallHostExists,
				"Failed while calling update_vertica because host(s) already installed: %s", m[1])
			return d.removeExistingHosts(ctx, pod, m[1])
		}
		d.VRec.EVRec.Event(d.Vdb, corev1.EventTypeWarning, events.InstallFailed,
			"Failed while calling update_vertica")
		return ctrl.Result{}, fmt.Errorf("failed to call update_vertica to add new hosts: %w", err)
	}
	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.InstallSucceeded,
		"Successfully called update_vertica to add new hosts and it took %s", time.Since(start))

	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, pods)

	// Invalidate the pod facts cache since its out of date due to the install
	d.PFacts.Invalidate()

	return ctrl.Result{}, d.createInstallIndicators(ctx, pods)
}

// getInstallTargets finds the list of hosts/pods that we will call update_vertica with.
func (d *InstallReconciler) getInstallTargets(ctx context.Context) ([]*PodFact, error) {
	podList := make([]*PodFact, 0, len(d.PFacts.Detail))
	for _, v := range d.PFacts.Detail {
		if v.isInstalled.IsFalse() && v.dbExists.IsFalse() {
			podList = append(podList, v)

			if v.hasStaleAdmintoolsConf {
				if _, _, err := d.PRunner.ExecInPod(ctx, v.name, ServerContainer, d.genCmdRemoveOldConfig()...); err != nil {
					return podList, fmt.Errorf("failed to remove old admintools.conf: %w", err)
				}
			}
		}
	}
	return podList, nil
}

// createInstallIndicators will create the install indicator file for all pods passed in
func (d *InstallReconciler) createInstallIndicators(ctx context.Context, pods []*PodFact) error {
	for _, v := range pods {
		compat21Node, err := d.fetchCompat21NodeNum(ctx, v)
		if err != nil {
			return fmt.Errorf("failed to extract compat21 node name: %w", err)
		}
		// Create the install indicator file. This is used to know that this
		// instance of the vdb has called update_vertica for this pod. The
		// /opt/vertica/config is backed by a PV, so it is possible that we
		// see state in there for a prior instance of the vdb. We use the
		// UID of the vdb to know the current instance.
		d.Log.Info("create installer indicator file", "Pod", v.name)
		cmd := d.genCmdCreateInstallIndicator(compat21Node)
		if stdout, _, err := d.PRunner.ExecInPod(ctx, v.name, ServerContainer, cmd...); err != nil {
			return fmt.Errorf("failed to create installer indicator with command '%s', output was '%s': %w", cmd, stdout, err)
		}
	}
	return nil
}

// fetchCompat21NodeNum will figure out the compat21 node name that was assigned to the given pod
func (d *InstallReconciler) fetchCompat21NodeNum(ctx context.Context, pf *PodFact) (string, error) {
	cmd := []string{
		"bash", "-c", fmt.Sprintf("grep -E '^node[0-9]{4} = %s,' %s", pf.podIP, paths.AdminToolsConf),
	}
	var stdout string
	var err error
	if stdout, _, err = d.PRunner.ExecInPod(ctx, pf.name, ServerContainer, cmd...); err != nil {
		return "", fmt.Errorf("failed to find compat21 node for IP '%s', output was '%s': %w", pf.podIP, stdout, err)
	}
	re := regexp.MustCompile(`^(node\d{4}) = .*`)
	match := re.FindAllStringSubmatch(stdout, 1)
	if len(match) > 0 && len(match[0]) > 0 {
		return match[0][1], nil
	}
	return "", fmt.Errorf("could not find compat21 node in output")
}

// genCmdInstall generates the command to run to install vertica at a list of nodes
func (d *InstallReconciler) genCmdInstall(pods []*PodFact, licensePath string) []string {
	hosts := make([]string, 0, len(pods))
	for _, pod := range pods {
		hosts = append(hosts, pod.dnsName)
	}

	updateVerticaCmd := []string{
		"sudo", "/opt/vertica/sbin/update_vertica",
		"--license", licensePath,
		"--accept-eula",
		"--failure-threshold", "NONE",
		"--dba-user-password-disabled",
		"--no-system-configuration",
		"--no-package-checks",
		"--point-to-point",
		"--data-dir", d.Vdb.Spec.Local.DataPath,
		"--add-hosts", strings.Join(hosts, ","),
	}
	if podsAllHaveIPv6(pods) {
		return append(updateVerticaCmd, "--ipv6")
	}
	return updateVerticaCmd
}

// genCmdCreateInstallIndicator generates the command to create the install indicator file
func (d *InstallReconciler) genCmdCreateInstallIndicator(compat21Node string) []string {
	// The install indicator file has the UID of the vdb. This allows us to know
	// that we are working with a different life in the vdb is ever recreated.
	return []string{"bash", "-c", fmt.Sprintf("echo %s > %s", compat21Node, paths.GenInstallerIndicatorFileName(d.Vdb))}
}

// genCmdRemoveOldConfig generates the command to remove the old admintools.conf file
func (d *InstallReconciler) genCmdRemoveOldConfig() []string {
	return []string{
		"mv",
		paths.AdminToolsConf,
		fmt.Sprintf("%s.uid.%s", paths.AdminToolsConf, string(d.Vdb.UID)),
	}
}

// findPodToInstallFrom will look at the facts and figure out the pod to run the installer from
func (d *InstallReconciler) findPodToInstallFrom() types.NamespacedName {
	// Find the first pod that has already run the installer. Keep track of the
	// last runnable pod that didn't an install. This is a fall back that we use
	// in case we don't find any pods that have had their cfg bootstrapped.
	var lastRunablePod types.NamespacedName
	for k, v := range d.PFacts.Detail {
		if v.isInstalled.IsTrue() {
			return k
		} else if v.isPodRunning {
			lastRunablePod = k
		}
	}
	return lastRunablePod
}

// anyPodsNotRunning checks if any pods were found not to be running
func (d *InstallReconciler) anyPodsNotRunning() bool {
	for _, v := range d.PFacts.Detail {
		if !v.isPodRunning {
			return true
		}
	}
	return false
}

// removeExistingHosts is called when the install fails due to hosts already being
// in the cluster.  This can happen if the operator is killed before it has a
// chance to create the install indicator file.  We will repair it by removing
// the host to allow a subsequent install to be successful.
func (d *InstallReconciler) removeExistingHosts(ctx context.Context, adminPod types.NamespacedName,
	existingHosts string) (ctrl.Result, error) {
	// The existing hosts comes in the form: '10.244.1.120'.  We need to find
	// the pod that corresponds to this IP so that we can create a command to
	// remove them.  Each pod we find will be added to the existingPods list.
	existingPods := []*PodFact{}
	for _, quotedHost := range strings.Split(existingHosts, " ") {
		host := strings.Trim(quotedHost, "'") // Remove ' that surrounds it
		for _, pod := range d.PFacts.Detail {
			if pod.podIP == host {
				existingPods = append(existingPods, pod)
			}
		}
	}

	if len(existingPods) > 0 {
		cmd := genCmdUninstall(existingPods)
		if _, _, err := d.PRunner.ExecInPod(ctx, adminPod, ServerContainer, cmd...); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Requeue so that we try the install again now that we uninstalled the
	// hosts that were already part of the cluster.
	return ctrl.Result{Requeue: true}, nil
}
