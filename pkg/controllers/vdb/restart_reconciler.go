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
	"math"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// Amount of time to wait after a restart failover before doing another requeue.
	RequeueWaitTimeInSeconds = 10
	// Percent of livenessProbe time to wait when requeuing due to waiting on
	// livenessProbe. This is just a heuristic we use to avoid going into a long
	// exponential backoff wait for the livenessProbe to fail.
	PctOfLivenessProbeWait = 0.25
)

// RestartReconciler will ensure each pod has a running vertica process
type RestartReconciler struct {
	VRec            *VerticaDBReconciler
	Log             logr.Logger
	Vdb             *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner         cmds.PodRunner
	PFacts          *PodFacts
	InitiatorPod    types.NamespacedName // The pod that we run admin commands from
	InitiatorPodIP  string               // The IP of the initiating pod
	RestartReadOnly bool                 // Whether to restart nodes that are in read-only mode
	Dispatcher      vadmin.Dispatcher
	ConfigParamsGenerator
}

// MakeRestartReconciler will build a RestartReconciler object
func MakeRestartReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts, restartReadOnly bool,
	dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &RestartReconciler{
		VRec:            vdbrecon,
		Log:             log.WithName("RestartReconciler"),
		Vdb:             vdb,
		PRunner:         prunner,
		PFacts:          pfacts,
		RestartReadOnly: restartReadOnly,
		Dispatcher:      dispatcher,
		ConfigParamsGenerator: ConfigParamsGenerator{
			VRec: vdbrecon,
			Log:  log.WithName("RestartReconciler"),
			Vdb:  vdb,
		},
	}
}

// Reconcile will ensure each pod is UP in the vertica sense.
// On success, each node will have a running vertica process.
func (r *RestartReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if !r.Vdb.Spec.AutoRestartVertica {
		err := vdbstatus.UpdateCondition(ctx, r.VRec.Client, r.Vdb,
			vapi.MakeCondition(vapi.AutoRestartVertica, metav1.ConditionFalse, "Detected"),
		)
		return ctrl.Result{}, err
	}

	err := vdbstatus.UpdateCondition(ctx, r.VRec.Client, r.Vdb,
		vapi.MakeCondition(vapi.AutoRestartVertica, metav1.ConditionTrue, "Detected"),
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// If the create/revive database process fails, we skip restarting the cluster to redo the create/revive process.
	// restart reconciler is only skipped for VClusterOps.
	// In Admintools, the IP is cached in admintool.conf and needs to be updated.
	if vmeta.UseVClusterOps(r.Vdb.Annotations) {
		isSet := r.Vdb.IsStatusConditionTrue(vapi.DBInitialized)
		if !isSet {
			r.Log.Info("Skipping restart reconciler since create_db or revive_db failed")
			return ctrl.Result{}, nil
		}
	}

	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// We have two paths.  If the entire cluster is down we have separate
	// admin commands to run.  Cluster operations only apply if the entire
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
	// Check if cluster start needs to include all of the pods.
	if r.Vdb.IsKSafety0() &&
		r.PFacts.countNotRestartablePods(vmeta.UseVClusterOps(r.Vdb.Annotations)) > 0 {
		// For k-safety 0, we need all of the pods because the absence of one
		// will cause us not to have enough pods for cluster quorum.
		r.Log.Info("Waiting for all installed pods to be running before attempt a cluster restart")
		return ctrl.Result{Requeue: true}, nil
	}

	// Find an initiator pod. You must pick one that has no vertica process running.
	// This is needed to be able to start the primaries when secondary read-only
	// nodes could be running.
	if ok := r.setInitiatorPod(r.PFacts.findPodToRunAdminCmdOffline); !ok {
		r.Log.Info("No initiator pod found to run admin command. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	// Re-IP needs to collect all nodes' IPs. When using vclusterops, we do not want to
	// restart transient nodes because there is not a config file for vclusterops to retrieve
	// transient nodes' IPs easily. However, using admintools, we can get the correct nodes'
	// IPs easily from admintools.conf. As a result, we exclude transient pods from the pods
	// to restart for vclusterops.
	downPods := r.PFacts.findRestartablePods(r.RestartReadOnly,
		!vmeta.UseVClusterOps(r.Vdb.Annotations), /* restartTransient */
		true /* restartPendingDelete */)

	// Kill any read-only vertica process that may still be running. This does
	// not include any rogue process that is no longer communicating with
	// spread; these are killed by the liveness probe. Read-only nodes need to
	// be killed because we need to restart vertica on them so they join the new
	// cluster and can gain write access.
	if res, err := r.killReadOnlyProcesses(ctx, downPods); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// If any of the pods have finished the startupProbe, we need to wait for
	// the livenessProbe to kill them before starting. If we don't do this, we
	// run the risk of having the livenessProbe delete the pod while we
	// are doing the startup. The startupProbe has a much longer timeout and can
	// accommodate a slow startup.
	if _, pc, err := r.filterNonActiveStartupProbe(ctx, downPods); err != nil {
		return ctrl.Result{}, err
	} else if pc != 0 {
		r.Log.Info("Some pods have active livenessProbes. Waiting for them to be rescheduled before trying a restart.",
			"podCount", pc)
		return r.makeResultForLivenessProbeWait(ctx)
	}

	// Similar to above, wait for any pods that are just slow starting. They
	// probably have a large catalog. So, its best to wait it out. The health
	// probes will eventually kill them if they can't make any progress.
	if _, pc := r.filterSlowStartup(downPods); pc != 0 {
		r.Log.Info("Some pods are slow starting up. Waiting for them to finish or abort before trying a cluster restart",
			"podCount", pc)
		return r.makeResultForLivenessProbeWait(ctx)
	}

	if err := r.acceptEulaIfMissing(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// re_ip nodes. This is done ahead of the db check in case we need to update
	// the IP of nodes that have been installed but not yet added to the db.
	if res, err := r.reipNodes(ctx, r.getReIPPods(false)); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// If no db, there is nothing to restart so we can exit.
	if !r.PFacts.doesDBExist() {
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
	// can't be restarted. Also, skip pending delete as they may be partially
	// removed already, preventing restart from succeding. They will be removed
	// shortly anyway so makes little sense to restart them.
	downPods := r.PFacts.findRestartablePods(r.RestartReadOnly,
		false /* restartTransient */, false /* restartPendingDelete */)
	// This is too make sure all pods have signed they EULA before running
	// admintools on any of them.
	if err := r.acceptEulaIfMissing(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if ok := r.setInitiatorPod(r.PFacts.findPodToRunAdminCmdAny); !ok {
		r.Log.Info("No initiator pod found for admin command. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	if len(downPods) > 0 {
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

	// Find any pods that need to have their IP updated.
	if res, err := r.reipNodes(ctx, r.getReIPPods(true)); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{Requeue: r.shouldRequeueIfPodsNotRunning()}, nil
}

// restartPods restart the down pods using an admin command
func (r *RestartReconciler) restartPods(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	// Reduce the pod list according to the cluster node state
	downPods, res, err := r.removePodsWithClusterUpState(ctx, pods)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	if len(downPods) == 0 {
		r.Log.Info("Pods are down but the cluster state doesn't show that yet. Requeue the reconciliation.")
		return r.makeResultForLivenessProbeWait(ctx)
	}

	// Kill any read-only vertica processes so we can restart them with full
	// write access. If any pods are killed, we will requeue.
	if res, err2 := r.killReadOnlyProcesses(ctx, downPods); verrors.IsReconcileAborted(res, err2) {
		return res, err2
	}

	var pc int
	downPods, pc, err = r.filterNonActiveStartupProbe(ctx, downPods)
	if err != nil {
		return ctrl.Result{}, err
	}
	if len(downPods) == 0 {
		r.Log.Info("Some pod(s) have active livenessProbes. "+
			"Waiting for them to be rescheduled before trying a restart.", "podCount", pc)
		return r.makeResultForLivenessProbeWait(ctx)
	}

	downPods, _ = r.filterSlowStartup(downPods)
	if len(downPods) == 0 {
		r.Log.Info("Some pod(s) are still starting up. Waiting for them to " +
			"finish or abort (via health probes) before trying to restart again")
		return r.makeResultForLivenessProbeWait(ctx)
	}

	if res, err := r.execRestartPods(ctx, downPods); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Invalidate the cached pod facts now that some pods have restarted.
	r.PFacts.Invalidate()

	// Schedule a requeue if we detected some down pods aren't down according to
	// the cluster state.
	if len(pods) > len(downPods) {
		return r.makeResultForLivenessProbeWait(ctx)
	}
	return ctrl.Result{}, nil
}

// removePodsWithClusterUpState will see if the pods in the down list are
// down according to the cluster state. This will return a new pod list with the
// pods that aren't considered down removed.
func (r *RestartReconciler) removePodsWithClusterUpState(ctx context.Context, pods []*PodFact) ([]*PodFact, ctrl.Result, error) {
	clusterState, res, err := r.fetchClusterNodeStatus(ctx, pods)
	if verrors.IsReconcileAborted(res, err) {
		return nil, res, err
	}
	i := 0
	// Remove any item from pods where the state is UP
	for _, pod := range pods {
		state, ok := clusterState[pod.vnodeName]
		if !ok || state != vadmin.StateUp {
			pods[i] = pod
			i++
		}
	}
	return pods[:i], ctrl.Result{}, nil
}

// fetchClusterNodeStatus gets the node status (UP/DOWN) from the cluster.
// This differs from the pod facts in that it is the cluster-wide state (aka
// SELECT * FROM NODES). It is possible for a pod to be down, but it doesn't
// show up as down in the cluster state.  Even then, there is still a chance
// that this may report a node is UP but not yet accepting connections because
// it could doing the initialization phase.
func (r *RestartReconciler) fetchClusterNodeStatus(ctx context.Context, pods []*PodFact) (map[string]string, ctrl.Result, error) {
	opts := []fetchnodestate.Option{
		fetchnodestate.WithInitiator(r.InitiatorPod, r.InitiatorPodIP),
	}
	for i := range pods {
		opts = append(opts, fetchnodestate.WithHost(pods[i].vnodeName, pods[i].podIP))
	}
	return r.Dispatcher.FetchNodeState(ctx, opts...)
}

// execRestartPods will execute the AT command and event recording for restart pods.
func (r *RestartReconciler) execRestartPods(ctx context.Context, downPods []*PodFact) (ctrl.Result, error) {
	podNames := make([]string, 0, len(downPods))
	for _, pods := range downPods {
		podNames = append(podNames, pods.name.Name)
	}

	opts := []restartnode.Option{
		restartnode.WithInitiator(r.InitiatorPod, r.InitiatorPodIP),
	}
	for i := range downPods {
		opts = append(opts, restartnode.WithHost(downPods[i].vnodeName, downPods[i].podIP))
	}

	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.NodeRestartStarted,
		"Starting database restart node of the following pods: %s", strings.Join(podNames, ", "))
	start := time.Now()
	labels := metrics.MakeVDBLabels(r.Vdb)
	res, err := r.Dispatcher.RestartNode(ctx, opts...)
	elapsedTimeInSeconds := time.Since(start).Seconds()
	metrics.NodesRestartDuration.With(labels).Observe(elapsedTimeInSeconds)
	metrics.NodesRestartAttempt.With(labels).Inc()
	if verrors.IsReconcileAborted(res, err) {
		metrics.NodesRestartFailed.With(labels).Inc()
		return res, err
	}
	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.NodeRestartSucceeded,
		"Successfully restarted database nodes and it took %ds", int(elapsedTimeInSeconds))
	return ctrl.Result{}, nil
}

// reipNodes will update the catalogs with new IPs for a set of pods.
// If it detects that no IPs are changing, then no re_ip is done.
func (r *RestartReconciler) reipNodes(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	if len(pods) == 0 {
		r.Log.Info("No pods qualify for possible re-ip.")
		return ctrl.Result{}, nil
	}
	opts := []reip.Option{
		reip.WithInitiator(r.InitiatorPod, r.InitiatorPodIP),
	}
	// If a communal path is set, include all of the EON parameters.
	if r.Vdb.Spec.Communal.Path != "" {
		// build communal storage params if there is not one
		if r.ConfigurationParams == nil {
			res, err := r.ConstructConfigParms(ctx)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}
		}
		opts = append(opts,
			reip.WithCommunalPath(r.Vdb.GetCommunalPath()),
			reip.WithConfigurationParams(r.ConfigurationParams.GetMap()),
		)
	}
	for i := range pods {
		if !pods[i].isPodRunning {
			r.Log.Info("Not all pods are running. Need to requeue restart reconciler.", "pod", pods[i].name)
			return ctrl.Result{Requeue: true}, nil
		}
		// Add the current host. Note, when using vclusterOps integration,
		// compat21NodeName won't be available. It is passed in incase we need
		// to use legacy admintools APIs.
		opts = append(opts, reip.WithHost(pods[i].vnodeName, pods[i].compat21NodeName, pods[i].podIP))
	}
	return r.Dispatcher.ReIP(ctx, opts...)
}

// restartCluster will call start database. It is assumed that the cluster has
// already run re_ip.
func (r *RestartReconciler) restartCluster(ctx context.Context, downPods []*PodFact) (ctrl.Result, error) {
	opts := []startdb.Option{
		startdb.WithInitiator(r.InitiatorPod, r.InitiatorPodIP),
	}
	// If a communal path is set, include all of the EON parameters.
	if r.Vdb.Spec.Communal.Path != "" {
		// build communal storage params if there is not one
		if r.ConfigurationParams == nil {
			res, err := r.ConstructConfigParms(ctx)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}
		}
		opts = append(opts,
			startdb.WithCommunalPath(r.Vdb.GetCommunalPath()),
			startdb.WithConfigurationParams(r.ConfigurationParams.GetMap()),
		)
	}
	for i := range downPods {
		opts = append(opts, startdb.WithHost(downPods[i].podIP))
	}
	r.VRec.Event(r.Vdb, corev1.EventTypeNormal, events.ClusterRestartStarted,
		"Starting restart of the cluster")
	start := time.Now()
	labels := metrics.MakeVDBLabels(r.Vdb)
	res, err := r.Dispatcher.StartDB(ctx, opts...)
	elapsedTimeInSeconds := time.Since(start).Seconds()
	metrics.ClusterRestartDuration.With(labels).Observe(elapsedTimeInSeconds)
	metrics.ClusterRestartAttempt.With(labels).Inc()
	if verrors.IsReconcileAborted(res, err) {
		metrics.ClusterRestartFailure.With(labels).Inc()
		return res, err
	}
	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.ClusterRestartSucceeded,
		"Successfully restarted the cluster and it took %ds", int(elapsedTimeInSeconds))
	return ctrl.Result{}, err
}

// killReadOnlyProcesses will remove any running vertica processes that are
// currently in read-only.  At this point, we have determined that the read-only
// nodes need to be shutdown so we can restart them to have full write access.
// We requeue the iteration if anything is killed so that status is updated
// before starting a restart; this is done for the benefit of PD purposes and
// stability in the restart test.
func (r *RestartReconciler) killReadOnlyProcesses(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	killedAtLeastOnePid := false
	for _, pod := range pods {
		// Only killing read-only vertica processes
		if !pod.readOnly {
			continue
		}
		const killMarker = "Killing process"
		// When NMA in sidecar is enabled we must remove the startup conf file
		// after killing the process to allow vertica to restart gracefully
		rmCmd := fmt.Sprintf("rm -rf %s", paths.StartupConfFile)
		killCmd := fmt.Sprintf("for pid in $(pgrep ^vertica$); do echo \"%s $pid\"; kill -n SIGKILL $pid; done", killMarker)
		cmd := []string{
			"bash", "-c",
			fmt.Sprintf("%s && %s", killCmd, rmCmd),
		}
		// Avoid all errors since the process may not even be running
		if stdout, _, err := r.PRunner.ExecInPod(ctx, pod.name, names.ServerContainer, cmd...); err != nil {
			return ctrl.Result{}, err
		} else if strings.Contains(stdout, killMarker) {
			killedAtLeastOnePid = true
		}
	}
	if killedAtLeastOnePid {
		// We are going to requeue if killed at least one process.  This is for
		// the benefit of the status reconciler, so that we don't treat it as
		// an up node anymore.
		r.Log.Info("Requeue.  Killed at least one read-only vertica process.")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// filterNonActiveStartupProbe returns a new pod list with the pods that
// have already finished the startupProbe filtered out. It also returns the
// number of pods that were removed. This is important because we don't want to
// restart any pod that has an active livelinessProbe. The pods are likely to
// get deleted part way through the restart.
func (r *RestartReconciler) filterNonActiveStartupProbe(ctx context.Context,
	pods []*PodFact) (newPodList []*PodFact, removedCount int, err error) {
	newPodList = []*PodFact{}
	var startupActive bool
	for i := range pods {
		startupActive, err = r.isStartupProbeActive(ctx, pods[i].name)
		if err != nil {
			return
		} else if !startupActive {
			r.Log.Info("Not restarting pod because its startupProbe is not active anymore. "+
				"Wait for livenessProbe to reschedule the pod", "pod", pods[i].name)
			continue
		}
		newPodList = append(newPodList, pods[i])
	}
	removedCount = len(pods) - len(newPodList)
	return
}

// filterSlowStartup removes any pods that are still in the process of starting
// up. We want to not consider them as candidates to startup. We would need to
// kill the vertica pid. Rather we let the health probes do that, which can be
// tuned to how long you want to wait for.
func (r *RestartReconciler) filterSlowStartup(pods []*PodFact) (newPodList []*PodFact, removedCount int) {
	for i := range pods {
		if pods[i].startupInProgress {
			continue
		}
		newPodList = append(newPodList, pods[i])
	}
	removedCount = len(pods) - len(newPodList)
	return
}

// getRequeueTimeoutForLivenessProbeWait will return the time to requeue if
// waiting for a livenessProbe to reschedule a pod.
func (r *RestartReconciler) makeResultForLivenessProbeWait(ctx context.Context) (ctrl.Result, error) {
	// If the restart reconciler is going to requeue because it has to wait for
	// the livenessProbe, we don't want to use the exponential backoff. That
	// could result in waiting too long for the requeue. Instead, we are going
	// to use a percentage of the total livenessProbe timeout.
	pn := names.GenPodName(r.Vdb, &r.Vdb.Spec.Subclusters[0], 0)
	pod := corev1.Pod{}
	if err := r.VRec.Client.Get(ctx, pn, &pod); err != nil {
		if k8sErrors.IsNotFound(err) {
			r.Log.Info("Could not read sample pod for livenessProbe timeout. Default to exponential backoff",
				"podName", pn)
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
	cnts := pod.Spec.Containers
	probe := cnts[names.GetServerContainerIndexInSlice(cnts)].LivenessProbe
	if probe == nil {
		// For backwards compatibility, if the probe isn't set, then we just
		// return a simple requeue with exponential backoff.
		return ctrl.Result{Requeue: true}, nil
	}
	timeToWait := int(float32(probe.PeriodSeconds*probe.FailureThreshold) * PctOfLivenessProbeWait)
	const MinWaitTime = 10
	return ctrl.Result{
		RequeueAfter: time.Second * time.Duration(int(math.Max(float64(timeToWait), MinWaitTime))),
	}, nil
}

// isStartupProbeActive will check if the given pod name has an active
// startupProbe.
func (r *RestartReconciler) isStartupProbeActive(ctx context.Context, nm types.NamespacedName) (bool, error) {
	pod := &corev1.Pod{}
	if err := r.VRec.Client.Get(ctx, nm, pod); err != nil {
		r.Log.Info("Failed to fetch the pod", "pod", nm, "err", err)
		return false, err
	}
	// If the pod doesn't have a livenessProbe then we always return true. This
	// can happen if we are in the middle of upgrading the operator.
	cnts := pod.Spec.Containers
	probe := cnts[names.GetServerContainerIndexInSlice(cnts)].LivenessProbe
	if probe == nil {
		r.Log.Info("Pod doesn't have a livenessProbe. Okay to restart", "pod", nm)
		return true, nil
	}
	// Check the container status of the server. There is a state in there
	// (Started) that indicates if the startupProbe is still active. Note, the
	// order of the containerStatusus can be in any order. They don't follow the
	// container definition order.
	for i := range pod.Status.ContainerStatuses {
		if pod.Status.ContainerStatuses[i].Name == names.ServerContainer {
			cstatStarted := pod.Status.ContainerStatuses[i].Started
			r.Log.Info("Pod container status", "pod", nm, "started", cstatStarted)
			return cstatStarted == nil || !*cstatStarted, nil
		}
	}
	// If no container status, then we assume startupProbe hasn't completed yet.
	return true, nil
}

// setInitiatorPod will set r.InitiatorPod if not already set.
// Caller can indicate whether there is a requirement that it must be run from a
// pod that is current not running the vertica daemon.
func (r *RestartReconciler) setInitiatorPod(findFunc func() (*PodFact, bool)) bool {
	// If we haven't done so already, figure out the pod that will serve as the
	// initiator for the command.
	if r.InitiatorPod == (types.NamespacedName{}) {
		initPod, ok := findFunc()
		if !ok {
			return false
		}
		r.InitiatorPod = initPod.name
		r.InitiatorPodIP = initPod.podIP
	}
	return true
}

// shouldRequeueIfPodsNotRunning is a helper function that will determine
// whether a requeue of the reconcile is necessary because some pods are not yet
// running.
func (r *RestartReconciler) shouldRequeueIfPodsNotRunning() bool {
	if r.PFacts.countNotRestartablePods(vmeta.UseVClusterOps(r.Vdb.Annotations)) > 0 {
		r.Log.Info("Requeue since some pods needed by restart are not yet running.")
		return true
	}
	return false
}

// acceptEulaIfMissing is a wrapper function that calls another function that
// accepts the end user license agreement.
func (r *RestartReconciler) acceptEulaIfMissing(ctx context.Context) error {
	// The EULA is specific to admintools based deployments. Skipping for
	// vcluster.
	if vmeta.UseVClusterOps(r.Vdb.Annotations) {
		return nil
	}
	return acceptEulaIfMissing(ctx, r.PFacts, r.PRunner)
}

// getReIPPods will return the list of pods that may need a re-ip. Factors that
// can affect the list is the restart type (cluster vs node) and usage of
// vclusterOps.
func (r *RestartReconciler) getReIPPods(isRestartNode bool) []*PodFact {
	// For restart node, we only re-ip nodes that won't be restarted. This is
	// necessary to keep installed-only nodes up to date in admintools.conf. For
	// this reason, we can skip if using vclusterOps.
	if isRestartNode {
		if vmeta.UseVClusterOps(r.Vdb.Annotations) {
			return nil
		}
		return r.PFacts.findReIPPods(dBCheckOnlyWithoutDBs)
	}
	// For cluster restart, we re-ip all nodes that have been added to the DB.
	// And if using admintools, we also need to re-ip installed pods that
	// haven't been added to the db to keep admintools.conf in-sync.
	if vmeta.UseVClusterOps(r.Vdb.Annotations) {
		return r.PFacts.findReIPPods(dBCheckOnlyWithDBs)
	}
	return r.PFacts.findReIPPods(dBCheckAny)
}
