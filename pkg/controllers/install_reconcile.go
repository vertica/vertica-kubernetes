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
	"io/ioutil"
	"os"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/lithammer/dedent"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/atconf"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// InstallReconciler will handle reconcile for install of vertica
type InstallReconciler struct {
	VRec     *VerticaDBReconciler
	Log      logr.Logger
	Vdb      *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner  cmds.PodRunner
	PFacts   *PodFacts
	ATWriter atconf.Writer
}

// MakeInstallReconciler will build and return the InstallReconciler object.
func MakeInstallReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &InstallReconciler{
		VRec:     vdbrecon,
		Log:      log,
		Vdb:      vdb,
		PRunner:  prunner,
		PFacts:   pfacts,
		ATWriter: atconf.MakeClusterWriter(log, vdb, prunner),
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

	if err := d.acceptEulaIfMissing(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// SPILLY - put these checks in a list
	if err := d.checkConfigDir(ctx); err != nil {
		return ctrl.Result{}, err
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

	installedPods := d.PFacts.findInstalledPods()
	ipsToInstall := []string{}
	for _, p := range pods {
		ipsToInstall = append(ipsToInstall, p.podIP)
	}
	installPod := types.NamespacedName{}
	if len(installedPods) != 0 {
		installPod = d.findPodToInstallFrom()
	}

	atConfTempFile, err := d.ATWriter.AddHosts(ctx, installPod, ipsToInstall)
	if err != nil {
		return ctrl.Result{}, err
	}
	// SPILLY - ensure we cleanup/remove temp file if we don't return success from AddHosts
	defer os.Remove(atConfTempFile)

	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)
	if err := d.distributeAdmintoolsConf(ctx, atConfTempFile); err != nil {
		return ctrl.Result{}, err
	}
	installedPods = append(installedPods, pods...)
	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)

	// Invalidate the pod facts cache since its out of date due to the install
	d.PFacts.Invalidate()

	return ctrl.Result{}, d.createInstallIndicators(ctx, pods)
}

// acceptEulaIfMissing will accept the end user license agreement if any pods have not yet signed it
func (d *InstallReconciler) acceptEulaIfMissing(ctx context.Context) error {
	for _, p := range d.PFacts.Detail {
		if !p.eulaAccepted.IsFalse() || !p.isPodRunning {
			continue
		}
		if err := d.acceptEulaInPod(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// checkConfigDir will check that certain directories in /opt/vertica/config
// exists and are writable by dbadmin
func (d *InstallReconciler) checkConfigDir(ctx context.Context) error {
	for _, p := range d.PFacts.Detail {
		if !p.isPodRunning {
			continue
		}
		if !p.logrotateIsWritable {
			// We enforce this in the docker entrypoint of the container too.  But
			// we have this here for backwards compatibility for images 11.0 or older.
			_, _, err := d.PRunner.ExecInPod(ctx, p.name, ServerContainer,
				"sudo", "chown", "-R", "dbadmin:verticadba", "/opt/vertica/config/logrotate")
			if err != nil {
				return err
			}
		}

		if !p.configShareExists {
			_, _, err := d.PRunner.ExecInPod(ctx, p.name, ServerContainer,
				"mkdir", "/opt/vertica/config/share")
			if err != nil {
				return err
			}
		}
	}
	return nil
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

// distributeAdmintoolsConf will copy the d.ATConfTempFile to all of the pods
func (d *InstallReconciler) distributeAdmintoolsConf(ctx context.Context, atConfTempFile string) error {
	for _, p := range d.PFacts.Detail {
		if !p.isPodRunning {
			continue
		}
		_, _, err := d.PRunner.CopyToPod(ctx, p.name, ServerContainer, atConfTempFile, paths.AdminToolsConf)
		if err != nil {
			return err
		}
	}
	return nil
}

// createInstallIndicators will create the install indicator file for all pods passed in
func (d *InstallReconciler) createInstallIndicators(ctx context.Context, pods []*PodFact) error {
	for _, v := range pods {
		compat21Node, err := d.fetchCompat21NodeNum(ctx, v)
		if err != nil {
			return fmt.Errorf("failed to extract compat21 node name: %w", err)
		}
		// SPILLY - search through usage of update_vertica or UpdateVertica (function names) and update
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

// SPILLY - test with ipv6

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

// acceptEulaInPod will run a script that will accept the eula in the given pod
func (d *InstallReconciler) acceptEulaInPod(ctx context.Context, pf *PodFact) error {
	tmp, err := ioutil.TempFile("", "accept_eula.py.")
	if err != nil {
		return err
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	// A python script that will accept the eula
	acceptEulaPython := `
		import vertica.shared.logging
		import vertica.tools.eula_checker
		vertica.shared.logging.setup_admintool_logging()
		vertica.tools.eula_checker.EulaChecker().write_acceptance()
	`
	_, err = tmp.WriteString(dedent.Dedent(acceptEulaPython))
	if err != nil {
		return err
	}
	tmp.Close()

	const inContPyFn = "/opt/vertica/config/accept_eula.py"
	_, _, err = d.PRunner.CopyToPod(ctx, pf.name, ServerContainer, tmp.Name(), inContPyFn)
	if err != nil {
		return err
	}

	_, _, err = d.PRunner.ExecInPod(ctx, pf.name, ServerContainer, "/opt/vertica/oss/python3/bin/python3", inContPyFn)
	if err != nil {
		return err
	}
	return nil
}
