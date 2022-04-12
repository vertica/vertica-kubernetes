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
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
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
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &DBAddNodeReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
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
	if d.PFacts.doesDBExist() == tristate.False {
		return ctrl.Result{Requeue: true}, nil
	}

	for i := range d.Vdb.Spec.Subclusters {
		if res, err := d.reconcileSubcluster(ctx, &d.Vdb.Spec.Subclusters[i]); verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return ctrl.Result{}, nil
}

// reconcileSubcluster will reconcile a single subcluster.  Add node will be
// triggered if we determine that it hasn't been run.
func (d *DBAddNodeReconciler) reconcileSubcluster(ctx context.Context, sc *vapi.Subcluster) (ctrl.Result, error) {
	addNodePods, unknownState := d.PFacts.findPodsWithMissingDB(sc.Name)

	// We want to group all of the add nodes in a single admintools call.
	// Doing so limits the impact on any running queries.  So if there is at
	// least one pod with unknown state, we requeue until that pod is
	// running before we proceed with the admintools call.
	if unknownState {
		d.Log.Info("Requeue add node because some pods were not running")
		return ctrl.Result{Requeue: true}, nil
	}
	if len(addNodePods) > 0 {
		res, err := d.runAddNode(ctx, addNodePods)
		return res, err
	}
	return ctrl.Result{}, nil
}

// runAddNode will add nodes to the given subcluster
func (d *DBAddNodeReconciler) runAddNode(ctx context.Context, pods []*PodFact) (ctrl.Result, error) {
	atPod, ok := d.PFacts.findPodToRunVsql(false, "")
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
	}

	debugDumpAdmintoolsConf(ctx, d.PRunner, atPod.name)

	if stdout, err := d.runAddNodeForPod(ctx, pods, atPod); err != nil {
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

	return ctrl.Result{}, nil
}

// runAddNodeForPod will execute the command to add a single node to the cluster
// Returns the stdout from the command.
func (d *DBAddNodeReconciler) runAddNodeForPod(ctx context.Context, pods []*PodFact, atPod *PodFact) (string, error) {
	podNames := genPodNames(pods)
	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.AddNodeStart,
		"Calling 'admintools -t db_add_node' for pod(s) '%s'", podNames)
	start := time.Now()
	cmd := d.genAddNodeCommand(pods)
	stdout, _, err := d.PRunner.ExecAdmintools(ctx, atPod.name, names.ServerContainer, cmd...)
	if err != nil {
		switch {
		case isLicenseLimitError(stdout):
			d.VRec.EVRec.Event(d.Vdb, corev1.EventTypeWarning, events.AddNodeLicenseFail,
				"You cannot add more nodes to the database.  You have reached the limit allowed by your license.")
		default:
			d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeWarning, events.AddNodeFailed,
				"Failed when calling 'admintools -t db_add_node' for pod(s) '%s'", podNames)
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

// genAddNodeCommand returns the command to run to add nodes to the cluster.
func (d *DBAddNodeReconciler) genAddNodeCommand(pods []*PodFact) []string {
	hostNames := make([]string, 0, len(pods))
	for _, pod := range pods {
		hostNames = append(hostNames, pod.dnsName)
	}

	return []string{
		"-t", "db_add_node",
		"--hosts", strings.Join(hostNames, ","),
		"--database", d.Vdb.Spec.DBName,
		"--subcluster", pods[0].subcluster,
		"--noprompt",
	}
}
