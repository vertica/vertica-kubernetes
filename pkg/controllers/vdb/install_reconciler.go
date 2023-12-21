/*
 (c) Copyright [2021-2023] Open Text.
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
	"strings"

	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/atconf"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/httpconf"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
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
		Log:      log.WithName("InstallReconciler"),
		Vdb:      vdb,
		PRunner:  prunner,
		PFacts:   pfacts,
		ATWriter: atconf.MakeFileWriter(log, vdb, prunner),
	}
}

// Reconcile will ensure Vertica is installed and running in the pods.
func (d *InstallReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for vclusterops deployment
	if vmeta.UseVClusterOps(d.Vdb.Annotations) {
		return ctrl.Result{}, nil
	}

	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// The reconcile loop works by collecting all of the facts about the running
	// pods. We then analyze those facts to determine a course of action to take.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return d.installForAdmintools(ctx)
}

// installForAdmintools will go through the install phase for admintools.
// It will look at the collected facts and determine the course of action
func (d *InstallReconciler) installForAdmintools(ctx context.Context) (ctrl.Result, error) {
	// We can only proceed with install if all of the installed pods are
	// running.  This ensures we can properly sync admintools.conf.
	if ok, podNotRunning := d.PFacts.anyInstalledPodsNotRunning(); ok {
		d.Log.Info("At least one installed pod isn't running.  Aborting the install.", "pod", podNotRunning)
		return ctrl.Result{Requeue: true}, nil
	}
	if ok, podNotRunning := d.PFacts.anyUninstalledTransientPodsNotRunning(); ok {
		d.Log.Info("At least one transient pod isn't running and doesn't have an install", "pod", podNotRunning)
		return ctrl.Result{Requeue: true}, nil
	}

	fns := []func(context.Context) error{
		d.acceptEulaIfMissing,
		d.createConfigDirsIfNecessary,
		// This has to be after accepting the EULA.  re_ip will not succeed if
		// the EULA is not accepted and a re_ip can happen before coming to this
		// reconcile function.  So if the pod is rescheduled after adding
		// hosts to the config, we have to know that a re_ip will succeed.
		d.addHostsToATConf,
		d.generateHTTPCerts,
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

	if d.VRec.OpCfg.DevMode {
		debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)
	}
	if err := distributeAdmintoolsConf(ctx, d.Vdb, d.VRec, d.PFacts, d.PRunner, atConfTempFile); err != nil {
		return err
	}
	installedPods = append(installedPods, pods...)
	if d.VRec.OpCfg.DevMode {
		debugDumpAdmintoolsConfForPods(ctx, d.PRunner, installedPods)
	}

	// Invalidate the pod facts cache since its out of date due to the install
	d.PFacts.Invalidate()

	return d.createInstallIndicators(ctx, pods)
}

// acceptEulaIfMissing is a wrapper function that calls another function that
// accepts the end user license agreement.
func (d *InstallReconciler) acceptEulaIfMissing(ctx context.Context) error {
	return acceptEulaIfMissing(ctx, d.PFacts, d.PRunner)
}

// createConfigDirsIfNecessary will check that certain directories in /opt/vertica/config
// exists and are writable by dbadmin
func (d *InstallReconciler) createConfigDirsIfNecessary(ctx context.Context) error {
	for _, p := range d.PFacts.Detail {
		if err := d.createConfigDirsForPodIfNecessary(ctx, p); err != nil {
			return err
		}
	}
	return nil
}

// generateHTTPCerts will generate the necessary config file to be able to start and
// communicate with the Vertica's https server.
func (d *InstallReconciler) generateHTTPCerts(ctx context.Context) error {
	for _, p := range d.PFacts.Detail {
		if !p.isPodRunning {
			continue
		}
		if !p.fileExists[paths.HTTPTLSConfFile] {
			frwt := httpconf.FileWriter{}
			secretName := names.GenNamespacedName(d.Vdb, d.Vdb.Spec.NMATLSSecret)
			fname, err := frwt.GenConf(ctx, d.VRec.Client, secretName)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed generating the %s file", paths.HTTPTLSConfFileName))
			}
			_, _, err = d.PRunner.CopyToPod(ctx, p.name, names.ServerContainer, fname,
				fmt.Sprintf("%s/%s", paths.HTTPTLSConfDir, paths.HTTPTLSConfFileName))
			_ = os.Remove(fname)
			if err != nil {
				return errors.Wrap(err, fmt.Sprintf("failed to copy %s to the pod %s", fname, p.name))
			}
			// Invalidate the pod facts cache since its out of date due the https generation
			d.PFacts.Invalidate()
		}
	}
	return nil
}

// getInstallTargets finds the list of hosts/pods that we need to initialize the config for
func (d *InstallReconciler) getInstallTargets(ctx context.Context) ([]*PodFact, error) {
	podList := make([]*PodFact, 0, len(d.PFacts.Detail))
	// We need to install pods in pod index order.  We do this because we can
	// determine if a pod has an installation by looking at the install count
	// for the subcluster.  For instance, if a subcluster of size 3 has no
	// installation, and pod-1 isn't running, we can only install pod-0.  Pod-2
	// needs to wait for the installation of pod-1.
	scMap := d.Vdb.GenSubclusterMap()
	for _, sc := range scMap {
		startPodIndex := int32(0)
		scStatus, ok := d.Vdb.FindSubclusterStatus(sc.Name)
		if ok {
			startPodIndex += scStatus.InstallCount()
		}
		for i := startPodIndex; i < sc.Size; i++ {
			pn := names.GenPodName(d.Vdb, sc, i)
			v, ok := d.PFacts.Detail[pn]
			if !ok {
				break
			}
			if v.isInstalled || v.dbExists {
				continue
			}
			// To ensure we only install pods in pod-index order, we stop the
			// install target search when we find a pod isn't running.
			if !v.isPodRunning {
				break
			}
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
		// Create the install indicator file. This is used to know that this
		// instance of the vdb has setup the config for this pod. The
		// /opt/vertica/config is backed by a PV, so it is possible that we
		// see state in there for a prior instance of the vdb. We use the
		// UID of the vdb to know the current instance.
		d.Log.Info("create installer indicator file", "Pod", v.name)
		cmd := d.genCmdCreateInstallIndicator(v)
		if stdout, _, err := d.PRunner.ExecInPod(ctx, v.name, names.ServerContainer, cmd...); err != nil {
			return fmt.Errorf("failed to create installer indicator with command '%s', output was '%s': %w", cmd, stdout, err)
		}
	}
	return nil
}

// genCmdCreateInstallIndicator generates the command to create the install indicator file
func (d *InstallReconciler) genCmdCreateInstallIndicator(pf *PodFact) []string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("grep -E '^node[0-9]{4} = %s,' %s", pf.podIP, paths.AdminToolsConf))
	sb.WriteString(" | head -1 | cut -d' ' -f1 | tee ")
	// The install indicator file has the UID of the vdb. This allows us to know
	// that we are working with a different life in the vdb is ever recreated.
	sb.WriteString(d.Vdb.GenInstallerIndicatorFileName())
	return []string{"bash", "-c", sb.String()}
}

// genCmdRemoveOldConfig generates the command to remove the old admintools.conf file
func (d *InstallReconciler) genCmdRemoveOldConfig() []string {
	return []string{
		"mv",
		paths.AdminToolsConf,
		fmt.Sprintf("%s.uid.%s", paths.AdminToolsConf, string(d.Vdb.UID)),
	}
}

// genCreateConfigDirsScript will create a script to be run in a pod to create
// the necessary dirs for install. This will return an empty string if nothing
// needs to happen.
func (d *InstallReconciler) genCreateConfigDirsScript(p *PodFact) (string, error) {
	var sb strings.Builder
	sb.WriteString("set -o errexit\n")
	numCmds := 0
	vinf, err := d.Vdb.MakeVersionInfoCheck()
	if err != nil {
		return "", err
	}
	// Logrotate setup is only required for versions before 24.1.0 of the database.
	// Starting from version 24.1.0, we use server-logrotate, which does not require logrotate setup.
	if !vinf.IsEqualOrNewer(vapi.InDatabaseLogRotateMinVersion) {
		if !p.dirExists[paths.ConfigLogrotatePath] {
			sb.WriteString(fmt.Sprintf("mkdir -p %s\n", paths.ConfigLogrotatePath))
			numCmds++
		}

		if !p.fileExists[paths.LogrotateATFile] {
			sb.WriteString(fmt.Sprintf("cp /home/dbadmin/logrotate/logrotate/%s %s\n", paths.LogrotateATFileName, paths.LogrotateATFile))
			numCmds++
		}

		if !p.fileExists[paths.LogrotateBaseConfFile] {
			sb.WriteString(fmt.Sprintf("cp /home/dbadmin/logrotate/%s %s\n", paths.LogrotateBaseConfFileName, paths.LogrotateBaseConfFile))
			numCmds++
		}
	}

	if !p.dirExists[paths.HTTPTLSConfDir] {
		sb.WriteString(fmt.Sprintf("mkdir -p %s\n", paths.HTTPTLSConfDir))
		numCmds++
	}

	if !p.dirExists[paths.ConfigSharePath] {
		sb.WriteString(fmt.Sprintf("mkdir %s\n", paths.ConfigSharePath))
		numCmds++
	}

	if !p.dirExists[paths.ConfigLicensingPath] {
		sb.WriteString(fmt.Sprintf("mkdir %s\n", paths.ConfigLicensingPath))
		numCmds++
	}

	if !p.dirExists[paths.ConfigLicensingPath] || !p.fileExists[paths.CELicenseFile] {
		sb.WriteString(fmt.Sprintf("cp /home/dbadmin/licensing/ce/%s %s 2>/dev/null || true\n", paths.CELicenseFileName, paths.CELicenseFile))
		numCmds++
	}

	if numCmds == 0 {
		return "", nil
	}
	return sb.String(), nil
}

// createConfigDirsForPodIfNecesssary will setup the config dirs for a single pod.
func (d *InstallReconciler) createConfigDirsForPodIfNecessary(ctx context.Context, p *PodFact) error {
	if !p.isPodRunning {
		return nil
	}
	tmp, err := os.CreateTemp("", "create-config-dirs.sh.")
	if err != nil {
		return err
	}
	defer tmp.Close()
	defer os.Remove(tmp.Name())

	script, err := d.genCreateConfigDirsScript(p)
	if err != nil {
		return err
	}
	if script == "" {
		return nil
	}
	_, err = tmp.WriteString(script)
	if err != nil {
		return err
	}
	tmp.Close()

	// Copy the script into the pod and execute it
	_, _, err = d.PRunner.CopyToPod(ctx, p.name, names.ServerContainer, tmp.Name(), paths.CreateConfigDirsScript,
		"bash", paths.CreateConfigDirsScript)
	if err != nil {
		return errors.Wrap(err, "failed to copy and execute the config dirs script")
	}
	return nil
}

// debugDumpAdmintoolsConfForPods will dump debug information for admintools.conf for a list of pods
func debugDumpAdmintoolsConfForPods(ctx context.Context, prunner cmds.PodRunner, pods []*PodFact) {
	for _, pod := range pods {
		prunner.DumpAdmintoolsConf(ctx, pod.name)
	}
}
