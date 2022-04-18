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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DBRemoveSubclusterReconciler will remove subclusters from the database
type DBRemoveSubclusterReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	ATPod   *PodFact // The pod that we run admintools from
}

// MakeDBRemoveSubclusterReconciler will build a DBRemoveSubclusterReconciler object
func MakeDBRemoveSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &DBRemoveSubclusterReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will remove any subcluster that no longer exists in the vdb.
func (d *DBRemoveSubclusterReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts, to find a pod to run AT and vsql commands from.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return d.removeExtraSubclusters(ctx)
}

// removeExtraSubclusters will compare subclusters in vertica with vdb and remove any extra ones
func (d *DBRemoveSubclusterReconciler) removeExtraSubclusters(ctx context.Context) (ctrl.Result, error) {
	finder := iter.MakeSubclusterFinder(d.VRec.Client, d.Vdb)
	// Find all subclusters not in the vdb.  These are the ones we want to remove.
	subclusters, err := finder.FindSubclusters(ctx, iter.FindNotInVdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(subclusters) > 0 {
		atPod, ok := d.PFacts.findPodToRunAdmintoolsAny()
		if !ok || !atPod.upNode {
			d.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
			return ctrl.Result{Requeue: true}, nil
		}
		d.ATPod = atPod

		if err := d.resetDefaultSubcluster(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	for i := range subclusters {
		if err := d.removeSubcluster(ctx, subclusters[i].Name); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// removeSubcluster will call admintools to remove the given subcluster from vertica
func (d *DBRemoveSubclusterReconciler) removeSubcluster(ctx context.Context, scName string) error {
	cmd := []string{
		"-t", "db_remove_subcluster",
		"--database", d.Vdb.Spec.DBName,
		"--subcluster", scName,
		"--noprompts",
	}

	stdout, _, err := d.PRunner.ExecAdmintools(ctx, d.ATPod.name, names.ServerContainer, cmd...)
	if err != nil {
		if strings.Contains(stdout, "No subcluster found") {
			// Nothing to do if the subcluster is already gone.
			d.Log.Info("Attempted to remove a subcluster that was already gone", "subcluster", scName)
			return nil
		}
		return err
	}
	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.SubclusterRemoved,
		"Removed subcluster '%s'", scName)
	return nil
}

// resetDefaultSubcluster will set the default subcluster to the first
// subcluster that exists in the vdb.  This step is necessary before removing
// any subclusters because you cannot remove the default subcluster.
func (d *DBRemoveSubclusterReconciler) resetDefaultSubcluster(ctx context.Context) error {
	defSc, err := d.getDefaultSubcluster(ctx)
	if err != nil {
		return err
	}

	// Check if the default subcluster is not in the map.  This implies we are
	// removing the default subcluster.
	scMap := d.Vdb.GenSubclusterMap()
	_, ok := scMap[defSc]
	if !ok {
		scFinder := iter.MakeSubclusterFinder(d.VRec.Client, d.Vdb)
		// We use the FindServices() API to get subclusters that already exist.
		// We can only change the default subcluster to one of those.
		svcs, err := scFinder.FindServices(ctx, iter.FindInVdb)
		if err != nil {
			return err
		}
		// If we don't find a service object we don't fail.  The attempt to
		// remove the default subcluster that we do later will fail.  That
		// provides a better error message than anything we do here.
		if len(svcs.Items) > 0 {
			return d.changeDefaultSubcluster(ctx, svcs.Items[0].Labels[builder.SubclusterNameLabel])
		}
	}
	return nil
}

// getDefaultSubcluster returns the name of the current default subcluster
func (d *DBRemoveSubclusterReconciler) getDefaultSubcluster(ctx context.Context) (string, error) {
	cmd := []string{
		"-tAc", "select subcluster_name from subclusters where is_default is true",
	}
	stdout, _, err := d.PRunner.ExecVSQL(ctx, d.ATPod.name, names.ServerContainer, cmd...)
	if err != nil {
		return "", err
	}

	lines := strings.Split(stdout, "\n")
	if len(lines) == 0 {
		return "", fmt.Errorf("no default subcluster found: %s", stdout)
	}
	return lines[0], nil
}

// changeDefaultSubcluster will change the current default subcluster to scName
func (d *DBRemoveSubclusterReconciler) changeDefaultSubcluster(ctx context.Context, scName string) error {
	cmd := []string{
		"-c", fmt.Sprintf(`alter subcluster "%s" set default`, scName),
	}
	_, _, err := d.PRunner.ExecVSQL(ctx, d.ATPod.name, names.ServerContainer, cmd...)
	return err
}
