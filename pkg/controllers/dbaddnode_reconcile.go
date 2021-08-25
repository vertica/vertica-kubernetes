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
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"yunion.io/x/pkg/tristate"
)

// DBAddNodeReconciler will ensure each pod is added to the database.
type DBAddNodeReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeDBAddNodeReconciler will build a DBAddNodeReconciler object
func MakeDBAddNodeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) ReconcileActor {
	return &DBAddNodeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will ensure a DB exists and create one if it doesn't
func (d *DBAddNodeReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// If no db exists, then we cannot do an db_add_node. Requeue as an earlier
	// reconciler should have created the db for us.
	if d.PFacts.doesDBExist() == tristate.False {
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range d.Vdb.Spec.Subclusters {
		sc := &d.Vdb.Spec.Subclusters[i]
		if exists := d.PFacts.anyPodsMissingDB(sc.Name); exists.IsTrue() {
			res, err := d.runAddNode(ctx, sc)
			if err != nil || res.Requeue {
				return res, err
			}
		}
	}

	return ctrl.Result{}, nil
}

// runAddNode will add nodes to the given subcluster
func (d *DBAddNodeReconciler) runAddNode(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	pods := d.PFacts.findPodsWithMissingDB(sc.Name)
	// The pods that are missing the db might not be running, so we requeue the
	// reconciliation so we don't miss this pod
	if len(pods) == 0 {
		return ctrl.Result{Requeue: true}, nil
	}

	atPod, ok := d.PFacts.findPodToRunVsql()
	if !ok {
		d.Log.Info("No pod found to run vsql and admintools from. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}

	if err := changeDepotPermissions(ctx, d.Vdb, d.PRunner, pods); err != nil {
		return ctrl.Result{}, err
	}

	for _, pod := range pods {
		// admintools will not cleanup the local directories after a failed attempt
		// to add node. So we ensure those directories are clear at each pod before
		// proceeding.
		if err := cleanupLocalFiles(ctx, d.Vdb, d.PRunner, pod.name); err != nil {
			return ctrl.Result{}, err
		}

		debugDumpAdmintoolsConf(ctx, d.PRunner, atPod.name)

		if stdout, err := d.runAddNodeForPod(ctx, pod, atPod); err != nil {
			// If we reached the node limit according to the license, end this
			// reconcile successfully. We don't want to fail and requeue because
			// this isn't going to get fixed until someone manually adds a new
			// license.
			if isLicenseLimitError(stdout) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		debugDumpAdmintoolsConf(ctx, d.PRunner, atPod.name)

		// Invalidate the cached pod facts now that some pods have a DB now.
		d.PFacts.Invalidate()
	}
	err := d.rebalanceShards(ctx, atPod, sc.Name)
	return ctrl.Result{}, err
}

// runAddNodeForPod will execute the command to add a single node to the cluster
// Returns the stdout from the command.
func (d *DBAddNodeReconciler) runAddNodeForPod(ctx context.Context, pod, atPod *PodFact) (string, error) {
	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.AddNodeStart,
		"Calling 'admintools -t db_add_node' for pod '%s'", pod.name.Name)
	start := time.Now()
	cmd := d.genAddNodeCommand(pod)
	stdout, _, err := d.PRunner.ExecAdmintools(ctx, atPod.name, names.ServerContainer, cmd...)
	if err != nil {
		switch {
		case isLicenseLimitError(stdout):
			d.VRec.EVRec.Event(d.Vdb, corev1.EventTypeWarning, events.AddNodeLicenseFail,
				"You cannot add more nodes to the database.  You have reached the limit allowed by your license.")
		default:
			d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeWarning, events.AddNodeFailed,
				"Failed when calling 'admintools -t db_add_node' from pod %s", pod.name.Name)
		}
	} else {
		d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.AddNodeSucceeded,
			"Successfully called 'admintools -t db_add_node' and it took %s", time.Since(start))
	}
	return stdout, err
}

// isLicenseLimitError returns true if the stdout contains the error about not enough licenses
func isLicenseLimitError(stdout string) bool {
	return strings.Contains(stdout, "Cannot create another node. The current license permits")
}

// rebalanceShards will execute the command to rebalance the shards
// between all the nodes(old and new)
func (d *DBAddNodeReconciler) rebalanceShards(ctx context.Context, atPod *PodFact, scName string) error {
	podName := atPod.name
	selectCmd := fmt.Sprintf("select rebalance_shards('%s')", scName)
	cmd := []string{
		"-tAc", selectCmd,
	}
	_, _, err := d.PRunner.ExecVSQL(ctx, podName, names.ServerContainer, cmd...)
	if err != nil {
		return err
	}

	return nil
}

// genAddNodeCommand returns the command to run to add nodes to the cluster.
func (d *DBAddNodeReconciler) genAddNodeCommand(pod *PodFact) []string {
	return []string{
		"-t", "db_add_node",
		"--hosts", pod.podIP,
		"--database", d.Vdb.Spec.DBName,
		"--subcluster", pod.subcluster,
		"--noprompt",
	}
}
