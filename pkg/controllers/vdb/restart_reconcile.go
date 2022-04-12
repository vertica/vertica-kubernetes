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
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// Amount of time to wait after a restart failover before doing another requeue.
	RequeueWaitTimeInSeconds = 10
	// The name of the IP map file that is used by re_ip.  re_ip is only ever used if the entire cluster is down.
	AdminToolsMapFile = "/opt/vertica/config/ipMap.txt"
	// Constant for an up node, this is taken from the STATE colume in NODES table
	StateUp = "UP"
)

// A map that does a lookup of a vertica node name to an IP address
type verticaIPLookup map[string]string

// RestartReconciler will ensure each pod has a running vertica process
type RestartReconciler struct {
	VRec            *VerticaDBReconciler
	Log             logr.Logger
	Vdb             *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner         cmds.PodRunner
	PFacts          *PodFacts
	ATPod           types.NamespacedName // The pod that we run admintools from
	RestartReadOnly bool                 // Whether to restart nodes that are in read-only mode
}

// MakeRestartReconciler will build a RestartReconciler object
func MakeRestartReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts, restartReadOnly bool) controllers.ReconcileActor {
	return &RestartReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts,
		RestartReadOnly: restartReadOnly}
}

// Reconcile will ensure each pod is UP in the vertica sense.
// On success, each node will have a running vertica process.
func (r *RestartReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if !r.Vdb.Spec.AutoRestartVertica {
		err := vdbstatus.UpdateCondition(ctx, r.VRec.Client, r.Vdb,
			vapi.VerticaDBCondition{Type: vapi.AutoRestartVertica, Status: corev1.ConditionFalse},
		)
		return ctrl.Result{}, err
	}

	err := vdbstatus.UpdateCondition(ctx, r.VRec.Client, r.Vdb,
		vapi.VerticaDBCondition{Type: vapi.AutoRestartVertica, Status: corev1.ConditionTrue},
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// We have two paths.  If the entire cluster is down we have separate
	// admintools commands to run.  Cluster operations only apply if the entire
	// vertica cluster is managed by k8s.  We skip that if initPolicy is
	// ScheduleOnly.
	if r.PFacts.getUpNodeAndNotReadOnlyCount() == 0 &&
		r.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyScheduleOnly {
		return r.reconcileCluster(ctx)
	}
	return r.reconcileNodes(ctx)
}

// reconcileCluster will handle restart when the entire cluster is down
func (r *RestartReconciler) reconcileCluster(ctx context.Context) (ctrl.Result, error) {
	r.Log.Info("Restart of entire cluster is needed")
	if r.PFacts.areAllPodsRunningAndZeroInstalled() {
		// Restart has nothing to do if nothing is installed
		r.Log.Info("All pods are running and none of them have an installation.  Nothing to restart.")
		return ctrl.Result{}, nil
	}
	if r.PFacts.countRunningAndInstalled() == 0 {
		// None of the running pods have Vertica installed.  Since there may be
		// a pod that isn't running that may need Vertica restarted we are going
		// to requeue to wait for that pod to start.
		r.Log.Info("Waiting for pods to come online that may need a Vertica restart")
		return ctrl.Result{Requeue: true}, nil
	}

	// Find an AT pod.  You must run with a pod that has no vertica process running.
	// This is needed to be able to start the primaries when secondary read-only
	// nodes could be running.
	if ok := r.setATPod(r.PFacts.findPodToRunAdmintoolsOffline); !ok {
		r.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	// Kill any vertica process that may still be running.  This includes a rogue
	// process that is no longer communicating with spread and process for
	// read-only nodes.  This is needed before re_ip, as re_ip can only work if
	// the database isn't running, which would be case if there are read-only
	// nodes.
	// Include transient nodes since we may need to run re-ip against them.
	downPods := r.PFacts.findRestartablePods(r.RestartReadOnly, true)
	if res, err := r.killOldProcesses(ctx, downPods); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// re_ip/start_db require all pods to be running that have run the
	// installation.  This check is done when we generate the map file
	// (genMapFile).
	if res, err := r.reipNodes(ctx, r.PFacts.findReIPPods(false)); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// If no db, there is nothing to restart so we can exit.
	if !r.PFacts.doesDBExist().IsTrue() {
		return ctrl.Result{}, nil
	}

	if res, err := r.restartCluster(ctx, downPods); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Invalidate the cached pod facts now that some pods have restarted.
	r.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// reconcileNodes will handle a subset of the pods.  It will try to restart any
// pods that are down.  And it will try to reip any pods that have been
// rescheduled since their install.
func (r *RestartReconciler) reconcileNodes(ctx context.Context) (ctrl.Result, error) {
	r.Log.Info("Restart of individual nodes is needed")
	// Find any pods that need to be restarted. These only include running pods.
	// If there is a pod that is not yet running, we leave them off for now.
	// When it does start running there will be another reconciliation cycle.
	// Always skip the transient pods since they only run the old image so they
	// can't be restarted.
	downPods := r.PFacts.findRestartablePods(r.RestartReadOnly, false)
	if len(downPods) > 0 {
		if ok := r.setATPod(r.PFacts.findPodToRunAdmintoolsAny); !ok {
			r.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
			return ctrl.Result{Requeue: true}, nil
		}

		if res, err := r.restartPods(ctx, downPods); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	// The rest of the steps depend on knowing the compat21 node name for the
	// pod.  If ScheduleOnly, we cannot reliable know that since the operator
	// didn't originate the install.  So we will skip the rest if running in
	// that mode.
	if r.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{Requeue: r.shouldRequeueIfPodsNotRunning()}, nil
	}

	// Find any pods that need to have their IP updated.  These are nodes that
	// have been installed but not yet added to a database.
	reIPPods := r.PFacts.findReIPPods(true)
	if len(reIPPods) > 0 {
		if ok := r.setATPod(r.PFacts.findPodToRunAdmintoolsAny); !ok {
			r.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
			return ctrl.Result{Requeue: true}, nil
		}
		if res, err := r.reipNodes(ctx, reIPPods); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{Requeue: r.shouldRequeueIfPodsNotRunning()}, nil
}

// restartPods restart the down pods using admintools
func (r *RestartReconciler) restartPods(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	// Reduce the pod list according to the cluster node state
	downPods, err := r.removePodsWithClusterUpState(ctx, pods)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(downPods) == 0 {
		// Pods are down but the cluster doesn't yet know that.  Requeue the reconciliation.
		return ctrl.Result{Requeue: true}, nil
	}
	vnodeList := genRestartVNodeList(downPods)
	ipList := genRestartIPList(downPods)

	if res, err := r.killOldProcesses(ctx, downPods); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	debugDumpAdmintoolsConf(ctx, r.PRunner, r.ATPod)

	cmd := r.genRestartNodeCmd(vnodeList, ipList)
	if stdout, err := r.execRestartPods(ctx, downPods, cmd); err != nil {
		if strings.Contains(stdout, "All nodes in the input are not down, can't restart") {
			// Vertica hasn't yet detected some nodes are done.  Give Vertica more time and requeue.
			return ctrl.Result{Requeue: false, RequeueAfter: time.Second * RequeueWaitTimeInSeconds}, nil
		}
		return ctrl.Result{}, fmt.Errorf("failed to restart pod(s) %w", err)
	}

	debugDumpAdmintoolsConf(ctx, r.PRunner, r.ATPod)

	// Invalidate the cached pod facts now that some pods have restarted.
	r.PFacts.Invalidate()

	// Schedule a requeue if we detected some down pods aren't down according to
	// the cluster state.
	return ctrl.Result{Requeue: len(pods) > len(downPods)}, nil
}

// removePodsWithClusterUpState will see if the pods in the down list are
// down according to the cluster state. This will return a new pod list with the
// pods that aren't considered down removed.
func (r *RestartReconciler) removePodsWithClusterUpState(ctx context.Context, pods []*PodFact) ([]*PodFact, error) {
	clusterState, err := r.fetchClusterNodeStatus(ctx)
	if err != nil {
		return nil, err
	}
	i := 0
	// Remove any item from pods where the state is UP
	for _, pod := range pods {
		state, ok := clusterState[pod.vnodeName]
		if !ok || state != StateUp {
			pods[i] = pod
			i++
		}
	}
	return pods[:i], nil
}

// fetchClusterNodeStatus gets the node status (UP/DOWN) from the cluster.
// This differs from the pod facts in that it is the cluster-wide state (aka
// SELECT * FROM NODES). It is possible for a pod to be down, but it doesn't
// show up as down in the cluster state.  Even then, there is still a chance
// that this may report a node is UP but not yet accepting connections because
// it could doing the initialization phase.
func (r *RestartReconciler) fetchClusterNodeStatus(ctx context.Context) (map[string]string, error) {
	cmd := []string{
		"-t", "list_allnodes",
	}
	stdout, _, err := r.PRunner.ExecAdmintools(ctx, r.ATPod, names.ServerContainer, cmd...)
	if err != nil {
		return nil, err
	}

	return r.parseClusterNodeStatus(stdout), nil
}

// parseClusterNodeStatus will parse the output from a AT -t list_allnodes call
func (r *RestartReconciler) parseClusterNodeStatus(stdout string) map[string]string {
	stateMap := map[string]string{}
	lines := strings.Split(stdout, "\n")
	const ColHeaderCount = 2
	if len(lines) <= ColHeaderCount {
		// Nothing to parse, return empty map
		return stateMap
	}
	// We skip the first two lines because they are for the header of the
	// output. The output that we are omitting looks like this:
	//  Node          | Host       | State | Version                 | DB
	// ---------------+------------+-------+-------------------------+----
	for _, line := range lines[ColHeaderCount:] {
		// Line is something like this:
		//   v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db
		cols := strings.Split(line, "|")
		const ListNodesColCount = 4
		if len(cols) < ListNodesColCount {
			continue
		}
		vnode := strings.Trim(cols[0], " ")
		state := strings.Trim(cols[2], " ")
		stateMap[vnode] = state
	}
	return stateMap
}

// execRestartPods will execute the AT command and event recording for restart pods.
func (r *RestartReconciler) execRestartPods(ctx context.Context, downPods []*PodFact, cmd []string) (string, error) {
	podNames := make([]string, 0, len(downPods))
	for _, pods := range downPods {
		podNames = append(podNames, pods.name.Name)
	}

	r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.NodeRestartStarted,
		"Calling 'admintools -t restart_node' to restart the following pods: %s", strings.Join(podNames, ", "))
	start := time.Now()
	stdout, _, err := r.PRunner.ExecAdmintools(ctx, r.ATPod, names.ServerContainer, cmd...)
	if err != nil {
		r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.NodeRestartFailed,
			"Failed while calling 'admintools -t restart_node'")
		return stdout, err
	}
	r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.NodeRestartSucceeded,
		"Successfully called 'admintools -t restart_node' and it took %s", time.Since(start))
	return stdout, nil
}

// reipNodes will run admintools -t re_ip against a set of pods.
// If it detects that no IPs are changing, then no re_ip is done.
func (r *RestartReconciler) reipNodes(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	var mapFileContents []string

	// We always use the compat21 nodes when generating the IP map.  We cannot
	// use the vnode because they are only set _after_ a node is added to a DB.
	// ReIP can be dealing with a mix -- some nodes that have been added to the
	// db and some that aren't.
	oldIPs, err := r.fetchOldIPsFromNode(ctx, r.ATPod)
	if err != nil {
		return ctrl.Result{}, err
	}

	mapFileContents, ipChanging, ok := r.genMapFile(oldIPs, pods)
	if !ok {
		r.Log.Info("Could not generate the map file contents from nodes.  Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	if !ipChanging {
		// no re-ip is necessary, the IP are not changing
		return ctrl.Result{}, nil
	}

	cmd := genMapFileUploadCmd(mapFileContents)
	if _, _, err := r.PRunner.ExecInPod(ctx, r.ATPod, names.ServerContainer, cmd...); err != nil {
		return ctrl.Result{}, err
	}

	// Prior to calling re_ip, dump out the state of admintools.conf for PD purposes
	debugDumpAdmintoolsConf(ctx, r.PRunner, r.ATPod)

	cmd = r.genReIPCommand()
	if _, _, err := r.PRunner.ExecAdmintools(ctx, r.ATPod, names.ServerContainer, cmd...); err != nil {
		// Log an event as failure to re_ip means we won't be able to bring up the database.
		r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.ReipFailed,
			"Attempt to run 'admintools -t re_ip' failed")
		return ctrl.Result{}, err
	}

	// Now that re_ip is done, dump out the state of admintools.conf to the log.
	debugDumpAdmintoolsConf(ctx, r.PRunner, r.ATPod)

	return ctrl.Result{}, nil
}

// restartCluster will call admintools -t start_db
// It is assumed that the cluster has already run re_ip.
func (r *RestartReconciler) restartCluster(ctx context.Context, downPods []*PodFact) (ctrl.Result, error) {
	cmd := r.genStartDBCommand(downPods)
	r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeNormal, events.ClusterRestartStarted,
		"Calling 'admintools -t start_db' to restart the cluster")
	start := time.Now()
	_, _, err := r.PRunner.ExecAdmintools(ctx, r.ATPod, names.ServerContainer, cmd...)
	if err != nil {
		r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.ClusterRestartFailed,
			"Failed while calling 'admintools -t start_db'")
		return ctrl.Result{}, err
	}
	r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.ClusterRestartSucceeded,
		"Successfully called 'admintools -t start_db' and it took %s", time.Since(start))
	return ctrl.Result{}, err
}

// genRestartVNodeList returns the vnodes of all of the hosts in downPods
func genRestartVNodeList(downPods []*PodFact) []string {
	hostList := []string{}
	for _, v := range downPods {
		hostList = append(hostList, v.vnodeName)
	}
	return hostList
}

// genRestartIPList returns the IPs of all of the hosts in downPods
func genRestartIPList(downPods []*PodFact) []string {
	ipList := []string{}
	for _, v := range downPods {
		ipList = append(ipList, v.podIP)
	}
	return ipList
}

// killOldProcesses will remove any running vertica processes.  At this point,
// we have determined the nodes are down, so we are cleaning up so that it
// doesn't impact the restart.  This may include killing a pod that is in the
// read-only state.  For this reason, we requeue the iteration if anything is
// killed so that status is updated before starting a restart; this is done for
// the benefit of PD purposes and stability in the restart test.
func (r *RestartReconciler) killOldProcesses(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	killedAtLeastOnePid := false
	for _, pod := range pods {
		const KillMarker = "Killing process"
		cmd := []string{
			"bash", "-c",
			fmt.Sprintf("for pid in $(pgrep ^vertica$); do echo \"%s $pid\"; kill -n SIGKILL $pid; done", KillMarker),
		}
		// Avoid all errors since the process may not even be running
		if stdout, _, err := r.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
			return ctrl.Result{}, err
		} else if strings.Contains(stdout, KillMarker) {
			killedAtLeastOnePid = true
		}
	}
	if killedAtLeastOnePid {
		// We are going to requeue if killed at least one process.  This is for
		// the benefit of the status reconciler, so that we don't treat it as
		// an up node anymore.
		r.Log.Info("Requeue.  Killed at least one vertica process.")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// genRestartNodeCmd returns the command to run to restart a pod
func (r *RestartReconciler) genRestartNodeCmd(vnodeList, ipList []string) []string {
	cmd := []string{
		"-t", "restart_node",
		"--database=" + r.Vdb.Spec.DBName,
		"--hosts=" + strings.Join(vnodeList, ","),
		"--new-host-ips=" + strings.Join(ipList, ","),
		"--noprompt",
	}
	if r.Vdb.Spec.RestartTimeout != 0 {
		cmd = append(cmd, fmt.Sprintf("--timeout=%d", r.Vdb.Spec.RestartTimeout))
	}
	return cmd
}

// parseNodesFromAdmintoolConf will parse out the vertica node and IP from admintools.conf output.
// The nodeText passed in is taken from a grep output of the node columns. As
// such, multiple lines are concatenated together with '\n'.
func parseNodesFromAdmintoolConf(nodeText string) verticaIPLookup {
	ips := make(verticaIPLookup)
	rs := `^(node\d{4}) = ([\d.:a-fA-F]+),`

	re := regexp.MustCompile(rs)
	for _, line := range strings.Split(nodeText, "\n") {
		match := re.FindAllStringSubmatch(line, 1)
		if len(match) > 0 && len(match[0]) >= 3 {
			ips[match[0][1]] = match[0][2]
		}
	}
	return ips
}

// fetchOldIPsFromNode will read a local admintools.conf and get the IPs from it.
// The IPs from an admintools.conf represent the *old* IPs. We store them in a
// map, where the lookup is by the node name. This function only handles
// compat21 node names.
func (r *RestartReconciler) fetchOldIPsFromNode(ctx context.Context, atPod types.NamespacedName) (verticaIPLookup, error) {
	cmd := r.genGrepNodeCmd()
	stdout, _, err := r.PRunner.ExecInPod(ctx, atPod, names.ServerContainer, cmd...)
	if err != nil {
		return verticaIPLookup{}, err
	}
	return parseNodesFromAdmintoolConf(stdout), nil
}

// genGrepNodeCmd returns the command to run to get the nodes from admintools.conf
// This function only handles grepping compat21 nodes.
func (r *RestartReconciler) genGrepNodeCmd() []string {
	return []string{
		"bash", "-c", fmt.Sprintf("grep --regexp='^node[0-9]' %s", paths.AdminToolsConf),
	}
}

// genMapFile generates the map file used by re_ip
// The list of old IPs are passed in. We combine that with the new IPs in the
// podfacts to generate the map file. The map file is returned as a list of
// strings. Its format is what is expected by admintools -t re_ip.
func (r *RestartReconciler) genMapFile(
	oldIPs verticaIPLookup, pods []*PodFact) (mapContents []string, ipChanging, ok bool) {
	mapContents = []string{}
	ipChanging = false
	ok = true

	if len(pods) == 0 {
		r.Log.Info("No pods qualify.  Need to requeue restart reconciler.")
		return mapContents, ipChanging, false
	}

	for _, pod := range pods {
		// If the pod is not running, then a re_ip is not possible because we won't know the new IP yet.
		if !pod.isPodRunning {
			r.Log.Info("Not all pods are running.  Need to requeue restart reconciler.", "pod", pod.name)
			return mapContents, ipChanging, false
		}
		nodeName := pod.compat21NodeName
		var oldIP string
		oldIP, ok = oldIPs[nodeName]
		// If we are missing the old IP, we skip and don't fail.  Re-ip allows
		// for a subset of the nodes and the host may already be removed from
		// the cluster anyway.
		if !ok {
			ok = true // reset to true in case this is the last pod
			continue
		}
		if oldIP != pod.podIP {
			ipChanging = true
		}
		mapContents = append(mapContents, fmt.Sprintf("%s %s", oldIP, pod.podIP))
	}
	return mapContents, ipChanging, ok
}

// genMapFileUploadCmd returns the command to run to upload the map file
func genMapFileUploadCmd(mapFileContents []string) []string {
	return []string{
		"bash", "-c", "cat > " + AdminToolsMapFile + "<<< '" + strings.Join(mapFileContents, "\n") + "'",
	}
}

// genReIPCommand will return the command to run for the re_ip command
func (r *RestartReconciler) genReIPCommand() []string {
	cmd := []string{
		"-t", "re_ip",
		"--file=" + AdminToolsMapFile,
		"--noprompt",
	}

	// In 11.1, we added a --force option to re_ip to allow us to run it while
	// some nodes are up.  This was done to support doing a reip while there are
	// read-only secondary nodes.
	vinf, ok := version.MakeInfoFromVdb(r.Vdb)
	if ok && vinf.IsEqualOrNewer(version.ReIPAllowedWithUpNodesVersion) {
		cmd = append(cmd, "--force")
	}

	return cmd
}

// genStartDBCommand will return the command for start_db
func (r *RestartReconciler) genStartDBCommand(downPods []*PodFact) []string {
	cmd := []string{
		"-t", "start_db",
		"--database=" + r.Vdb.Spec.DBName,
		"--noprompt",
	}
	if r.Vdb.Spec.IgnoreClusterLease {
		cmd = append(cmd, "--ignore-cluster-lease")
	}
	if r.Vdb.Spec.RestartTimeout != 0 {
		cmd = append(cmd, fmt.Sprintf("--timeout=%d", r.Vdb.Spec.RestartTimeout))
	}

	// In some versions, we can include a list of hosts to start.  This
	// parameter becomes important for online upgrade as we use this to start
	// the primaries while the secondary are in read-only.
	vinf, ok := version.MakeInfoFromVdb(r.Vdb)
	if ok && vinf.IsEqualOrNewer(version.StartDBAcceptsHostListVersion) {
		hostNames := []string{}
		for _, pod := range downPods {
			hostNames = append(hostNames, pod.podIP)
		}
		cmd = append(cmd, "--hosts", strings.Join(hostNames, ","))
	}

	return cmd
}

// setATPod will set r.ATPod if not already set.
// Caller can indicate whether there is a requirement that it must be run from a
// pod that is current not running the vertica daemon.
func (r *RestartReconciler) setATPod(findFunc func() (*PodFact, bool)) bool {
	// If we haven't done so already, figure out the pod to run admintools from.
	if r.ATPod == (types.NamespacedName{}) {
		atPod, ok := findFunc()
		if !ok {
			return false
		}
		r.ATPod = atPod.name
	}
	return true
}

// shouldRequeueIfPodsNotRunning is a helper function that will determine
// whether a requeue of the reconcile is necessary because some pods are not yet
// running.
func (r *RestartReconciler) shouldRequeueIfPodsNotRunning() bool {
	if r.PFacts.countNotRunning() > 0 {
		r.Log.Info("Requeue.  Some pods are not yet running.")
		return true
	}
	return false
}
