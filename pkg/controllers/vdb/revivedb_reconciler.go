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
	"fmt"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	"github.com/vertica/vertica-kubernetes/pkg/reviveplanner"
	vtypes "github.com/vertica/vertica-kubernetes/pkg/types"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/revivedb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReviveDBReconciler will revive a database if one doesn't exist in the vdb yet.
type ReviveDBReconciler struct {
	VRec                *VerticaDBReconciler
	Log                 logr.Logger
	Vdb                 *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner             cmds.PodRunner
	PFacts              *PodFacts
	Planr               *reviveplanner.Planner
	Dispatcher          vadmin.Dispatcher
	ConfigurationParams *vtypes.CiMap
}

// MakeReviveDBReconciler will build a ReviveDBReconciler object
func MakeReviveDBReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts,
	dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &ReviveDBReconciler{
		VRec:    vdbrecon,
		Log:     log.WithName("ReviveDBReconciler"),
		Vdb:     vdb,
		PRunner: prunner,
		PFacts:  pfacts,
		Planr: reviveplanner.MakePlanner(
			log,
			reviveplanner.ClusterConfigParserFactory(vmeta.UseVClusterOps(vdb.Annotations), log),
		),
		Dispatcher:          dispatcher,
		ConfigurationParams: vtypes.MakeCiMap(),
	}
}

// Reconcile will ensure a DB exists and revive one if it doesn't
func (r *ReviveDBReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Skip this reconciler entirely if the init policy is to create the DB.
	if r.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyRevive {
		return ctrl.Result{}, nil
	}

	// The remaining revive_db logic is driven from GenericDatabaseInitializer.
	// This exists to creation an abstraction that is common with create_db.
	g := GenericDatabaseInitializer{
		initializer: r,
		PRunner:     r.PRunner,
		PFacts:      r.PFacts,
		ConfigParamsGenerator: ConfigParamsGenerator{
			VRec:                r.VRec,
			Log:                 r.Log,
			Vdb:                 r.Vdb,
			ConfigurationParams: r.ConfigurationParams,
		},
	}
	return g.checkAndRunInit(ctx)
}

// execCmd will do the actual execution of revive DB.
// This handles logging of necessary events.
func (r *ReviveDBReconciler) execCmd(ctx context.Context, initiatorPod types.NamespacedName,
	hostList []string, podNames []types.NamespacedName) (ctrl.Result, error) {
	opts := r.genReviveOpts(initiatorPod, hostList, podNames)
	r.VRec.Event(r.Vdb, corev1.EventTypeNormal, events.ReviveDBStart, "Starting revive database")
	start := time.Now()
	if res, err := r.Dispatcher.ReviveDB(ctx, opts...); verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	r.VRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.ReviveDBSucceeded,
		"Successfully revived database. It took %s", time.Since(start).Truncate(time.Second))
	return ctrl.Result{}, nil
}

// preCmdSetup is going to run revive with --display-only then validate and
// fix-up any mismatch it finds.
func (r *ReviveDBReconciler) preCmdSetup(ctx context.Context, initiatorPod types.NamespacedName,
	initiatorIP string) (ctrl.Result, error) {
	// We need to delete any statefulsets that have pods with pending revisions.
	// This can happen if in an earlier iteration we changed the paths in pod.
	// Normally, these types of changes are rolled out via rolling upgrade. But
	// that depends on having the pod get to the ready state. That's not
	// possible because we haven't initialized the DB yet. So, we need to
	// reschedule before the revive.
	if res, err := r.deleteRevisionPendingSts(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Generate output to feed into the revive planner
	stdout, res, err := r.runRevivePrepass(ctx, initiatorPod, initiatorIP)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Run the revive planner that will check if everything is compatible and
	// may end up changing the vdb to make it compatible.
	return r.runRevivePlanner(ctx, stdout)
}

// postCmdCleanup is a no-op for revive.  This exists so that we can use the
// DatabaseInitializer interface.
func (r *ReviveDBReconciler) postCmdCleanup(_ context.Context) (ctrl.Result, error) {
	return ctrl.Result{}, nil
}

// getPodList gets a list of the pods we are going to use in revive db.
// If it was not able to generate a list, possibly due to a bad reviveOrder, it
// return false for the bool return value.
func (r *ReviveDBReconciler) getPodList() ([]*PodFact, bool) {
	// Build up a map that keeps track of the number of pods for each subcluster.
	// Entries in this map get decremented as we add pods to the pod list.
	scPodCounts := map[int]int32{}
	for i := range r.Vdb.Spec.Subclusters {
		scPodCounts[i] = r.Vdb.Spec.Subclusters[i].Size
	}

	// Helper to log an event when reviveOrder is found to be bad
	logBadReviveOrder := func(reason string) {
		r.VRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.ReviveOrderBad,
			"revive_db failed because the reviveOrder specified is bad: %s", reason)
	}

	// This is the pod list that we are going to create and return
	podList := []*PodFact{}

	// Helper to add pods to podList
	addPodsFromSubcluster := func(subclusterIndex int, podsToAdd int32) bool {
		sc := &r.Vdb.Spec.Subclusters[subclusterIndex]
		for j := int32(0); j < podsToAdd; j++ {
			podsLeft := scPodCounts[subclusterIndex]
			podIndex := sc.Size - podsLeft
			pn := names.GenPodName(r.Vdb, sc, podIndex)
			pf, ok := r.PFacts.Detail[pn]
			if !ok {
				logBadReviveOrder(fmt.Sprintf("pod '%s' not found", pn.Name))
				return false
			}
			podList = append(podList, pf)
			scPodCounts[subclusterIndex]--
		}
		return true
	}

	// Start building the pod list from the revive order
	for i := range r.Vdb.Spec.ReviveOrder {
		cur := r.Vdb.Spec.ReviveOrder[i]

		if cur.SubclusterIndex < 0 || cur.SubclusterIndex >= len(r.Vdb.Spec.Subclusters) {
			logBadReviveOrder(fmt.Sprintf("subcluster index '%d' out of bounds", cur.SubclusterIndex))
			return nil, false
		}

		podsToAdd := int32(cur.PodCount)
		podsLeft := scPodCounts[cur.SubclusterIndex]
		if podsLeft < podsToAdd || podsToAdd <= 0 {
			podsToAdd = podsLeft
		}

		if !addPodsFromSubcluster(cur.SubclusterIndex, podsToAdd) {
			return nil, false
		}
	}

	// Ensure we did not miss any pods.  This loop exists mainly for the case
	// where the reviveOrder is empty.
	for i := range r.Vdb.Spec.Subclusters {
		if !addPodsFromSubcluster(i, scPodCounts[i]) {
			return nil, false
		}
	}
	return podList, true
}

// findPodToRunInit will return a PodFact of the pod that should run the init
// command from
func (r *ReviveDBReconciler) findPodToRunInit() (*PodFact, bool) {
	return r.PFacts.findPodToRunAdminCmdOffline()
}

// genReviveOpts will return the options to use with the revive command
func (r *ReviveDBReconciler) genReviveOpts(initiatorPod types.NamespacedName,
	hostList []string, podName []types.NamespacedName) []revivedb.Option {
	opts := []revivedb.Option{
		revivedb.WithInitiator(initiatorPod),
		revivedb.WithPods(podName),
		revivedb.WithHosts(hostList),
		revivedb.WithDBName(r.Vdb.Spec.DBName),
	}
	if r.Vdb.IsEON() {
		opts = append(opts,
			revivedb.WithCommunalPath(r.Vdb.GetCommunalPath()),
			revivedb.WithCommunalStorageParams(paths.AuthParmsFile),
			revivedb.WithConfigurationParams(r.ConfigurationParams.GetMap()),
		)
	}
	if r.Vdb.GetIgnoreClusterLease() {
		opts = append(opts, revivedb.WithIgnoreClusterLease())
	}
	return opts
}

// genDescribeOpts will return the options to use with the describe db function
func (r *ReviveDBReconciler) genDescribeOpts(initiatorPod types.NamespacedName, initiatorIP string) []describedb.Option {
	return []describedb.Option{
		describedb.WithInitiator(initiatorPod, initiatorIP),
		describedb.WithDBName(r.Vdb.Spec.DBName),
		describedb.WithCommunalPath(r.Vdb.GetCommunalPath()),
		describedb.WithCommunalStorageParams(paths.AuthParmsFile),
		describedb.WithConfigurationParams(r.ConfigurationParams.GetMap()),
	}
}

// deleteRevisionPendingSts will delete any statefulset that has pods with pending revision update.
func (r *ReviveDBReconciler) deleteRevisionPendingSts(ctx context.Context) (ctrl.Result, error) {
	numStsDeleted := 0
	finder := iter.MakeSubclusterFinder(r.VRec.Client, r.Vdb)
	stss, err := finder.FindStatefulSets(ctx, iter.FindInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}
	for i := range stss.Items {
		if stss.Items[i].Status.CurrentRevision != stss.Items[i].Status.UpdateRevision {
			r.Log.Info("Deleting STS because it has not updated all of the pods", "name", stss.Items[i].Name,
				"currentRevision", stss.Items[i].Status.CurrentReplicas,
				"updateRevision", stss.Items[i].Status.UpdateRevision)
			err = r.VRec.Client.Delete(ctx, &stss.Items[i])
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to delete statefulset %s: %w", stss.Items[i].Name, err)
			}
			numStsDeleted++
		}
	}
	if numStsDeleted > 0 {
		r.Log.Info("Requeue to wait for deleted statefulsets to be regenerated")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// runRevivePrepass will run revive with --display-only to check for any
// preconditions that need to be met. The output of the run is returned so it
// can be analyzed by the revive planner.
func (r *ReviveDBReconciler) runRevivePrepass(ctx context.Context, initiatorPod types.NamespacedName,
	initiatorIP string) (string, ctrl.Result, error) {
	opts := r.genDescribeOpts(initiatorPod, initiatorIP)
	return r.Dispatcher.DescribeDB(ctx, opts...)
}

func (r *ReviveDBReconciler) runRevivePlanner(ctx context.Context, op string) (ctrl.Result, error) {
	// Parse the JSON output we get from the AT command.
	if err := r.Planr.Parser.Parse(op); err != nil {
		return ctrl.Result{}, err
	}
	msg, ok := r.Planr.IsCompatible()
	if !ok {
		r.VRec.Event(r.Vdb, corev1.EventTypeWarning, events.ReviveDBFailed, msg)
		return ctrl.Result{Requeue: true}, nil
	}

	nm := r.Vdb.ExtractNamespacedName()
	vdbChanged := false
	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		vdb := &vapi.VerticaDB{}
		if retryErr := r.VRec.Client.Get(ctx, nm, vdb); retryErr != nil {
			if errors.IsNotFound(retryErr) {
				r.Log.Info("VerticaDB resource not found. Ignoring since object must be deleted")
				return nil
			}
			return retryErr
		}

		var retryErr error
		vdbChanged, retryErr = r.Planr.ApplyChanges(vdb)
		if !vdbChanged {
			return nil
		}
		if retryErr != nil {
			return retryErr
		}

		r.Log.Info("Updating vdb from revive planner")
		return r.VRec.Client.Update(ctx, vdb)
	})

	// Always requeue if the vdb was changed in this function.
	return ctrl.Result{Requeue: vdbChanged}, err
}
