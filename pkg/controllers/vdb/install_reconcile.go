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
	"io/ioutil"
	"os"
	"regexp"

	"github.com/go-logr/logr"
	"github.com/lithammer/dedent"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/atconf"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/names"
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
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &InstallReconciler{
		VRec:     vdbrecon,
		Log:      log,
		Vdb:      vdb,
		PRunner:  prunner,
		PFacts:   pfacts,
		ATWriter: atconf.MakeFileWriter(log, vdb, prunner),
	}
}

// Reconcile will ensure Vertica is installed and running in the pods.
func (d *InstallReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// The reconcile loop works by collecting all of the facts about the running
	// pods. We then analyze those facts to determine a course of action to take.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}
	return d.analyzeFacts(ctx)
}

// analyzeFacts will look at the collected facts and determine the course of action
func (d *InstallReconciler) analyzeFacts(ctx context.Context) (ctrl.Result, error) {
	// We can only proceed with install if all of the pods are running.  This
	// ensures we can properly sync admintools.conf.
	if ok, podNotRunning := d.PFacts.anyPodsNotRunning(); ok {
		d.Log.Info("At least one pod isn't running.  Aborting the install.", "pod", podNotRunning)
		return ctrl.Result{Requeue: true}, nil
	}

	fns := []func(context.Context) error{
		d.acceptEulaIfMissing,
		d.checkConfigDir,
		// This has to be after accepting the EULA.  re_ip will not succeed if
		// the EULA is not accepted and a re_ip can happen before coming to this
		// reconcile function.  So if the pod is rescheduled after adding
		// hosts to the config, we have to know that a re_ip will succeed.
		d.addHostsToATConf,
	}
	for _, fn := range fns {
		if err := fn(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// addHostsToATConf will add hosts for any pods that have not yet bootstrapped the config.
func (d *InstallReconciler) addHostsToATConf(ctx context.Context) error {
	pods, err := d.getInstallTargets(ctx)
	if err != nil {
		return err
	}
	if len(pods) == 0 {
		return nil
	}

	installedPods := d.PFacts.findInstalledPods()
	ipsToInstall := []string{}
	for _, p := range pods {
		ipsToInstall = append(ipsToInstall, p.podIP)
	}
	installPod := types.NamespacedName{}
	if len(installedPods) != 0 {
		installPod, err = findATBasePod(d.Vdb, d.PFacts)
		if err != nil {
			return err
		}
	}

	atConfTempFile, err := d.ATWriter.AddHosts(ctx, installPod, ipsToInstall)
	if err != nil {
		return err
	}
	defer os.Remove(atConfTempFile)

	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)
	if err := distributeAdmintoolsConf(ctx, d.Vdb, d.VRec, d.PFacts, d.PRunner, atConfTempFile); err != nil {
		return err
	}
	installedPods = append(installedPods, pods...)
	debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)

	// Invalidate the pod facts cache since its out of date due to the install
	d.PFacts.Invalidate()

	return d.createInstallIndicators(ctx, pods)
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
		if p.configLogrotateExists && !p.configLogrotateWritable {
			// We enforce this in the docker entrypoint of the container too.  But
			// we have this here for backwards compatibility for the 11.0 image.
			// The 10.1.1 image doesn't even have logrotate, which is why we
			// first check if the directory exists.
			_, _, err := d.PRunner.ExecInPod(ctx, p.name, names.ServerContainer,
				"sudo", "chown", "-R", "dbadmin:verticadba", paths.ConfigLogrotatePath)
			if err != nil {
				return err
			}
		}

		if !p.configShareExists {
			_, _, err := d.PRunner.ExecInPod(ctx, p.name, names.ServerContainer,
				"mkdir", paths.ConfigSharePath)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// getInstallTargets finds the list of hosts/pods that we need to initialize the config for
func (d *InstallReconciler) getInstallTargets(ctx context.Context) ([]*PodFact, error) {
	podList := make([]*PodFact, 0, len(d.PFacts.Detail))
	for _, v := range d.PFacts.Detail {
		if v.isInstalled.IsFalse() && v.dbExists.IsFalse() {
			podList = append(podList, v)

			if v.hasStaleAdmintoolsConf {
				if _, _, err := d.PRunner.ExecInPod(ctx, v.name, names.ServerContainer, d.genCmdRemoveOldConfig()...); err != nil {
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
		// instance of the vdb has setup the config for this pod. The
		// /opt/vertica/config is backed by a PV, so it is possible that we
		// see state in there for a prior instance of the vdb. We use the
		// UID of the vdb to know the current instance.
		d.Log.Info("create installer indicator file", "Pod", v.name)
		cmd := d.genCmdCreateInstallIndicator(compat21Node)
		if stdout, _, err := d.PRunner.ExecInPod(ctx, v.name, names.ServerContainer, cmd...); err != nil {
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
	if stdout, _, err = d.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, cmd...); err != nil {
		return "", fmt.Errorf("failed to find compat21 node for IP '%s', output was '%s': %w", pf.podIP, stdout, err)
	}
	re := regexp.MustCompile(`^(node\d{4}) = .*`)
	match := re.FindAllStringSubmatch(stdout, 1)
	if len(match) > 0 && len(match[0]) > 0 {
		return match[0][1], nil
	}
	return "", fmt.Errorf("could not find compat21 node in output")
}

// genCmdCreateInstallIndicator generates the command to create the install indicator file
func (d *InstallReconciler) genCmdCreateInstallIndicator(compat21Node string) []string {
	// The install indicator file has the UID of the vdb. This allows us to know
	// that we are working with a different life in the vdb is ever recreated.
	return []string{"bash", "-c", fmt.Sprintf("echo %s > %s", compat21Node, d.Vdb.GenInstallerIndicatorFileName())}
}

// genCmdRemoveOldConfig generates the command to remove the old admintools.conf file
func (d *InstallReconciler) genCmdRemoveOldConfig() []string {
	return []string{
		"mv",
		paths.AdminToolsConf,
		fmt.Sprintf("%s.uid.%s", paths.AdminToolsConf, string(d.Vdb.UID)),
	}
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

	_, _, err = d.PRunner.CopyToPod(ctx, pf.name, names.ServerContainer, tmp.Name(), paths.EulaAcceptanceScript)
	if err != nil {
		return err
	}

	_, _, err = d.PRunner.ExecInPod(ctx, pf.name, names.ServerContainer, "/opt/vertica/oss/python3/bin/python3", paths.EulaAcceptanceScript)
	if err != nil {
		return err
	}
	return nil
}
