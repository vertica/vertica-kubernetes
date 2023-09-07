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
	"sort"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addnode"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DBAddNodeReconciler will ensure each pod is added to the database.
type DBAddNodeReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner    cmds.PodRunner
	PFacts     *PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeDBAddNodeReconciler will build a DBAddNodeReconciler object
func MakeDBAddNodeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts, dispatcher vadmin.Dispatcher,
) controllers.ReconcileActor {
	return &DBAddNodeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("DBAddNodeReconciler"),
		Vdb:        vdb,
		PRunner:    prunner,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

// Reconcile will ensure a DB exists and create one if it doesn't
func (d *DBAddNodeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// If no db exists, then we cannot do an db_add_node. Requeue as an earlier
	// reconciler should have created the db for us.
	if !d.PFacts.doesDBExist() {
		d.Log.Info("Database doesn't exist in db add node.  Requeue as it should have been created already")
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range d.Vdb.Spec.Subclusters {
		if res, err := d.reconcileSubcluster(ctx, &d.Vdb.Spec.Subclusters[i]); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// findAddNodePods will return a list of pods facts that require add node.
// It will only return pods if all of the ones with a missing DB can run add node.
// If at least one pod is found that is unavailable for add node, the search
// will abort.  The list will be ordered by pod index.
func (d *DBAddNodeReconciler) findAddNodePods(scName string) ([]*PodFact, ctrl.Result) {
	podList := []*PodFact{}
	for _, v := range d.PFacts.Detail {
		if v.subclusterName != scName {
			continue
		}
		if !v.dbExists {
			if !v.isPodRunning || !v.isInstalled {
				// We want to group all of the add nodes in a single admintools call.
				// Doing so limits the impact on any running queries.  So if there is at
				// least one pod that cannot run add node, we requeue until that pod is
				// available before we proceed with the admintools call.
				d.Log.Info("Requeue add node because some pods were not available", "pod", v.name, "isPodRunning",
					v.isPodRunning, "installed", v.isInstalled)
				return nil, ctrl.Result{Requeue: true}
			}
			podList = append(podList, v)
		}
	}
	// Return an ordered list by pod index for easier debugging
	sort.Slice(podList, func(i, j int) bool {
		return podList[i].dnsName < podList[j].dnsName
	})
	return podList, ctrl.Result{}
}

// reconcileSubcluster will reconcile a single subcluster.  Add node will be
// triggered if we determine that it hasn't been run.
func (d *DBAddNodeReconciler) reconcileSubcluster(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	addNodePods, res := d.findAddNodePods(sc.Name)
	if verrors.IsReconcileAborted(res, nil) {
		return res, nil
	}
	if len(addNodePods) > 0 {
		var err error
		res, err = d.runAddNode(ctx, addNodePods)
		return res, err
	}
	return ctrl.Result{}, nil
}

// runAddNode will add nodes to the given subcluster
func (d *DBAddNodeReconciler) runAddNode(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	initiatorPod, ok := d.PFacts.findPodToRunVsql(false, "")
	if !ok {
		d.Log.Info("No pod found to run vsql and admintools from. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	for _, pod := range pods {
		// admintools will not cleanup the local directories after a failed attempt
		// to add node. So we ensure those directories are clear at each pod before
		// proceeding.
		if err := prepLocalData(ctx, d.Vdb, d.PRunner, pod.name); err != nil {
			return ctrl.Result{}, err
		}
	}

	if err := d.runAddNodeForPod(ctx, pods, initiatorPod); err != nil {
		// If we reached the node limit according to the license, end this
		// reconcile successfully. We don't want to fail and requeue because
		// this isn't going to get fixed until someone manually adds a new
		// license.
		if _, ok := err.(*addnode.LicenseLimitError); ok {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Invalidate the cached pod facts now that some pods have a DB now.
	d.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// runAddNodeForPod will execute the command to add a single node to the cluster
// Returns the stdout from the command.
func (d *DBAddNodeReconciler) runAddNodeForPod(ctx context.Context, pods []*PodFact, initiatorPod *PodFact) error {
	podNames := genPodNames(pods)
	d.VRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.AddNodeStart,
		"Starting add database node for pod(s) '%s'", podNames)
	start := time.Now()
	opts := []addnode.Option{
		addnode.WithInitiator(initiatorPod.name, initiatorPod.podIP),
		addnode.WithSubcluster(pods[0].subclusterName),
	}
	for i := range pods {
		opts = append(opts, addnode.WithHost(pods[i].dnsName))
	}
	err := d.Dispatcher.AddNode(ctx, opts...)
	if err != nil {
		return err
	}
	d.VRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.AddNodeSucceeded,
		"Successfully added database nodes and it took %s", time.Since(start))
	return nil
}
