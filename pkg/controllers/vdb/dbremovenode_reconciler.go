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
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DBRemoveNodeReconciler will handle removing a node from the database during scale down.
type DBRemoveNodeReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner    cmds.PodRunner
	PFacts     *PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeDBRemoveNodeReconciler will build and return the DBRemoveNodeReconciler object.
func MakeDBRemoveNodeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &DBRemoveNodeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("DBRemoveNodeReconciler"),
		Vdb:        vdb,
		PRunner:    prunner,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

func (d *DBRemoveNodeReconciler) GetClient() client.Client {
	return d.VRec.Client
}

func (d *DBRemoveNodeReconciler) GetVDB() *vapi.VerticaDB {
	return d.Vdb
}

func (d *DBRemoveNodeReconciler) CollectPFacts(ctx context.Context) error {
	return d.PFacts.Collect(ctx, d.Vdb)
}

// Reconcile will handle calling remove node when scale down is detected.
//
// This reconcile function is meant to be called before we create/delete any
// kubernetes objects. It allows us to look at the state before applying
// everything in Vdb. We will know if we are scaling down by comparing the
// expected subcluster size with the current.
func (d *DBRemoveNodeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// There is a timing scenario where it's possible to skip the drain and just
	// proceed to remove the nodes. This can occur if the vdb scale down occurs
	// in the middle of a reconciliation.  This scale down will use the latest
	// info in the vdb, which may be newer than the state that the drain node
	// reconiler uses. This check has be close to where we decide about the
	// scale down.
	if changed, err := d.PFacts.HasVerticaDBChangedSinceCollection(ctx, d.Vdb); changed || err != nil {
		if changed {
			d.Log.Info("Requeue because vdb has changed since last pod facts collection",
				"oldResourceVersion", d.PFacts.VDBResourceVersion,
				"newResourceVersion", d.Vdb.ResourceVersion)
		}
		return ctrl.Result{Requeue: changed}, err
	}

	// Use the finder so that we check only the subclusters that are in the vdb.
	// Any nodes that are in subclusters that we are removing are handled by the
	// DBRemoveSubcusterReconciler.
	finder := iter.MakeSubclusterFinder(d.VRec.Client, d.Vdb)
	subclusters, err := finder.FindSubclusters(ctx, iter.FindInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	for i := range subclusters {
		if res, err := d.reconcileSubcluster(ctx, subclusters[i]); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileSubcluster Will handle reconcile for a single subcluster
// This will check for db_remove_node at a single cluster and handle it is needed.
func (d *DBRemoveNodeReconciler) reconcileSubcluster(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	return scaledownSubcluster(ctx, d, sc, d.removeNodesInSubcluster)
}

// removeNodesInSubcluster will call remove node for a range of pods that need to be scaled down
// It will determine the list of pods it can scale down. If any pods within the
// range could not be scaled down, then it will proceed with the nodes it can
// scale down and return indicating reconciliation needs to be requeued.
func (d *DBRemoveNodeReconciler) removeNodesInSubcluster(ctx context.Context, sc *vapi.Subcluster,
	startPodIndex, endPodIndex int32) (ctrl.Result, error) {
	podsToRemove, requeueNeeded := d.findPodsSuitableForScaleDown(sc, startPodIndex, endPodIndex)
	if len(podsToRemove) > 0 {
		initiatorPod, ok := d.PFacts.findPodToRunAdminCmdAny()
		if !ok {
			// Requeue since we couldn't find a running pod
			d.Log.Info("Requeue since we could not find a pod to run admintools")
			return ctrl.Result{Requeue: true}, nil
		}

		if err := d.runRemoveNode(ctx, initiatorPod, podsToRemove); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to call remove node: %w", err)
		}

		// We successfully called db_remove_node, invalidate the pod facts cache
		// so that it is refreshed the next time we need it.
		d.PFacts.Invalidate()

		// Log an event if the shard/node ratio gets to high
		d.VRec.checkShardToNodeRatio(d.Vdb, sc)
	}

	return ctrl.Result{Requeue: requeueNeeded}, nil
}

// runRemoveNode will run the admin command to remove the node
// This handles recording of the events.
func (d *DBRemoveNodeReconciler) runRemoveNode(ctx context.Context, initiatorPod *PodFact, pods []*PodFact) error {
	podNames := genPodNames(pods)
	d.VRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.RemoveNodesStart,
		"Starting database remove node for pods '%s'", podNames)
	start := time.Now()
	opts := []removenode.Option{
		removenode.WithInitiator(initiatorPod.name, initiatorPod.podIP),
	}
	for i := range pods {
		opts = append(opts, removenode.WithHost(pods[i].dnsName))
	}
	if err := d.Dispatcher.RemoveNode(ctx, opts...); err != nil {
		d.VRec.Event(d.Vdb, corev1.EventTypeWarning, events.RemoveNodesFailed,
			"Failed when calling database remove node")
		return err
	}
	d.VRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.RemoveNodesSucceeded,
		"Successfully removed nodes from database and it took %s", time.Since(start).Truncate(time.Second))
	return nil
}

// findPodsSuitableForScaleDown will return a list of host names that can be scaled down.
// If a pod was skipped that may require a scale down, then the bool return
// comes back as true. It is the callers responsibility to requeue a
// reconciliation if that is true.
func (d *DBRemoveNodeReconciler) findPodsSuitableForScaleDown(sc *vapi.Subcluster, startPodIndex, endPodIndex int32) ([]*PodFact, bool) {
	pods := []*PodFact{}
	requeueNeeded := false
	for podIndex := startPodIndex; podIndex <= endPodIndex; podIndex++ {
		removeNodePod := names.GenPodName(d.Vdb, sc, podIndex)
		podFact, ok := d.PFacts.Detail[removeNodePod]
		if !ok {
			d.Log.Info("Not able to get pod facts for pod.  Requeue iteration.", "pod", removeNodePod)
			requeueNeeded = true
			continue
		}
		if podFact.dbExists && !podFact.isPodRunning {
			d.Log.Info("Pod requires scale down but isn't running yet", "pod", removeNodePod)
			requeueNeeded = true
			continue
		}
		// Fine to skip if we never added a database to this pod
		if !podFact.dbExists {
			continue
		}
		pods = append(pods, podFact)
	}
	return pods, requeueNeeded
}
