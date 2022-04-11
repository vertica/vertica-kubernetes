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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/version"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DBAddSubclusterReconciler will create a new subcluster if necessary
type DBAddSubclusterReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
	ATPod   *PodFact // The pod that we run admintools from
}

type SubclustersSet map[string]bool

// MakeDBAddSubclusterReconciler will build a DBAddSubclusterReconciler object
func MakeDBAddSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &DBAddSubclusterReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will ensure a subcluster exists for each one defined in the vdb.
func (d *DBAddSubclusterReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts, to find a pod to run AT and vsql commands from.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	return d.addMissingSubclusters(ctx, d.Vdb.Spec.Subclusters)
}

// addMissingSubclusters will compare subclusters passed in and create any missing ones
func (d *DBAddSubclusterReconciler) addMissingSubclusters(ctx context.Context, scs []vapi.Subcluster) (ctrl.Result, error) {
	atPod, ok := d.PFacts.findPodToRunAdmintoolsAny()
	if !ok || !atPod.upNode {
		d.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
		return ctrl.Result{Requeue: true}, nil
	}
	d.ATPod = atPod

	subclusters, res, err := d.fetchSubclusters(ctx)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	for i := range scs {
		sc := &scs[i]
		_, ok := subclusters[sc.Name]
		if ok {
			continue
		}

		if err := d.createSubcluster(ctx, sc); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// fetchSubclusters will return a set of all of the subclusters that exist in vertica
func (d *DBAddSubclusterReconciler) fetchSubclusters(ctx context.Context) (SubclustersSet, ctrl.Result, error) {
	cmd := []string{
		"-tAc", "select distinct(subcluster_name) from subclusters",
	}
	stdout, _, err := d.PRunner.ExecVSQL(ctx, d.ATPod.name, names.ServerContainer, cmd...)
	if err != nil {
		return nil, ctrl.Result{}, err
	}

	return d.parseFetchSubclusterVsql(stdout), ctrl.Result{}, nil
}

// parseFetchSubclusterVsql will parse vsql output from a query to fetch all subclusters
func (d *DBAddSubclusterReconciler) parseFetchSubclusterVsql(stdout string) SubclustersSet {
	// The output is similar to this:
	// 	sc1\n
	//  sc2\n
	lines := strings.Split(stdout, "\n")
	subclusters := SubclustersSet{}
	for i := 0; i < len(lines); i++ {
		sc := strings.Trim(lines[i], " ")
		if sc == "" {
			continue
		}
		subclusters[sc] = true
	}
	return subclusters
}

// createSubcluster will create the given subcluster
func (d *DBAddSubclusterReconciler) createSubcluster(ctx context.Context, sc *vapi.Subcluster) error {
	cmd := []string{
		"-t", "db_add_subcluster",
		"--database", d.Vdb.Spec.DBName,
		"--subcluster", sc.Name,
	}

	// In v11, when adding a subcluster it defaults to a secondary.  Prior
	// versions default to a primary.  Use the correct switch, depending on what
	// version we are using.
	vinf, ok := version.MakeInfoFromVdb(d.Vdb)
	const DefaultSecondarySubclusterCreationVersion = "v11.0.0"
	if ok && vinf.IsEqualOrNewer(DefaultSecondarySubclusterCreationVersion) {
		if sc.IsPrimary {
			cmd = append(cmd, "--is-primary")
		}
	} else {
		if !sc.IsPrimary {
			cmd = append(cmd, "--is-secondary")
		}
	}

	_, _, err := d.PRunner.ExecAdmintools(ctx, d.ATPod.name, names.ServerContainer, cmd...)
	d.VRec.EVRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.SubclusterAdded,
		"Added new subcluster '%s'", sc.Name)
	return err
}
