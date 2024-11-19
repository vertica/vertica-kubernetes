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
	"strings"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/metrics"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DBRemoveSubclusterReconciler will remove subclusters from the database
type DBRemoveSubclusterReconciler struct {
	VRec                  *VerticaDBReconciler
	Log                   logr.Logger
	Vdb                   *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner               cmds.PodRunner
	PFacts                *podfacts.PodFacts
	ATPod                 *podfacts.PodFact // The pod that we run admintools from
	Dispatcher            vadmin.Dispatcher
	CalledInOnlineUpgrade bool // Indicate if the constructor is called from online upgrade reconciler
}

// MakeDBRemoveSubclusterReconciler will build a DBRemoveSubclusterReconciler object
func MakeDBRemoveSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	prunner cmds.PodRunner, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, calledInOnlineUpgrade bool) controllers.ReconcileActor {
	return &DBRemoveSubclusterReconciler{
		VRec:                  vdbrecon,
		Log:                   log.WithName("DBRemoveSubclusterReconciler"),
		Vdb:                   vdb,
		PRunner:               prunner,
		PFacts:                pfacts,
		Dispatcher:            dispatcher,
		CalledInOnlineUpgrade: calledInOnlineUpgrade,
	}
}

// Reconcile will remove any subcluster that no longer exists in the vdb.
func (d *DBRemoveSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy
	if d.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts, to find a pod to run AT and vsql commands from.
	if err := d.PFacts.Collect(ctx, d.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// There is a timing scenario where it's possible to skip the drain and just
	// proceed to remove the subcluster. This can occur if the vdb scale down
	// occurs in the middle of a reconciliation.  This scale down will use the
	// latest info in the vdb, which may be newer than the state that the drain
	// node reconiler uses. This check has be close to where we decide about the
	// scale down.
	if changed, err := d.PFacts.HasVerticaDBChangedSinceCollection(ctx, d.Vdb); changed || err != nil {
		if changed {
			d.Log.Info("Requeue because vdb has changed since last pod facts collection",
				"oldResourceVersion", d.PFacts.VDBResourceVersion,
				"newResourceVersion", d.Vdb.ResourceVersion)
		}
		return ctrl.Result{Requeue: changed}, err
	}

	if res, err := d.removeExtraSubclusters(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	return ctrl.Result{}, nil
}

// removeExtraSubclusters will compare subclusters in vertica with vdb and remove any extra ones
func (d *DBRemoveSubclusterReconciler) removeExtraSubclusters(ctx context.Context) (ctrl.Result, error) {
	finder := iter.MakeSubclusterFinder(d.VRec.Client, d.Vdb)
	// Find all subclusters not in the vdb.  These are the ones we want to remove.
	subclusters, err := finder.FindSubclusters(ctx, iter.FindNotInVdb, vapi.MainCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if len(subclusters) > 0 {
		atPod, ok := d.PFacts.FindPodToRunAdminCmdAny()
		if !ok || !atPod.GetUpNode() {
			d.Log.Info("No pod found to run admintools from. Requeue reconciliation.")
			return ctrl.Result{Requeue: true}, nil
		}
		d.ATPod = atPod

		if err := d.resetDefaultSubcluster(ctx); err != nil {
			return ctrl.Result{}, err
		}
	}

	for i := range subclusters {
		stat, ok := d.Vdb.FindSubclusterStatus(subclusters[i].Name)
		// We cannot find subcluster status for the subcluster. We will skip metric cleanup.
		if ok {
			// Clear out any metrics for the subcluster we are about to delete
			metrics.HandleSubclusterDelete(d.Vdb, stat.Oid, d.Log)
		} else {
			d.Log.Info("Skipping metric cleanup for subcluster removal as oid is unknown", "name", subclusters[i].Name)
		}

		if err := d.removeSubcluster(ctx, subclusters[i].Name); err != nil {
			return ctrl.Result{}, err
		}

		if err := d.updateSubclusterStatus(ctx, subclusters[i].Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to update subcluster status: %w", err)
		}

		if vmeta.UseVProxy(d.Vdb.Annotations) && subclusters[i].Size == 0 {
			// Remove client proxy deployment
			err := d.removeClientProxy(ctx, subclusters[i])
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		// We successfully called remove subcluster and updated the status, invalidate
		// the pod facts cache so that it is refreshed the next time we need it.
		d.PFacts.Invalidate()
	}
	return ctrl.Result{}, nil
}

// removeSubcluster will call an admin function to remove the given subcluster from vertica
func (d *DBRemoveSubclusterReconciler) removeSubcluster(ctx context.Context, scName string) error {
	// nodes' names and addresses in the subcluster to remove subcluster. These names and addresses
	// are the latest ones in the database, and vclusterOps will compare them with the ones in catalog.
	// If vclusterOps find catalog of the cluster has stale node addresses, it will use the correct
	// addresses in this map to do a re-ip before removing subcluster.
	nodeNameAddressMap := d.PFacts.FindNodeNameAndAddressInSubcluster(scName)

	nodesToPollSubs := []string{}
	// when we remove nodes in online upgrade, we don't need to check node subscriptions
	// on the nodes in old main cluster so we need to pass nodeToPollSubs to vclusterOps to
	// let vclusterOps only check node subscriptions on the nodes that are promoted from the
	// sandbox.
	if d.CalledInOnlineUpgrade {
		scNames := d.Vdb.GetSubclustersForReplicaGroup(vmeta.ReplicaGroupBValue)
		nodesToPollSubs = d.PFacts.FindNodeNamesInSubclusters(scNames)
	}

	err := d.Dispatcher.RemoveSubcluster(ctx,
		removesc.WithInitiator(d.ATPod.GetName(), d.ATPod.GetPodIP()),
		removesc.WithSubcluster(scName),
		// vclusterOps needs correct node names and addresses to do re-ip
		removesc.WithNodeNameAddressMap(nodeNameAddressMap),
		removesc.WithNodesToPollSubs(nodesToPollSubs),
	)
	if err != nil {
		return err
	}
	d.VRec.Eventf(d.Vdb, corev1.EventTypeNormal, events.SubclusterRemoved,
		"Removed subcluster '%s'", scName)
	return nil
}

// updateSubclusterStatus updates all of the given subcluster's nodes detail
// in the status
func (d *DBRemoveSubclusterReconciler) updateSubclusterStatus(ctx context.Context, scName string) error {
	refreshInPlace := func(vdb *vapi.VerticaDB) error {
		scMap := vdb.GenSubclusterStatusMap()
		scs := scMap[scName]
		if scs == nil {
			return nil
		}
		for _, p := range d.PFacts.Detail {
			if p.GetSubclusterName() == scName {
				if int(p.GetPodIndex()) < len(scs.Detail) {
					scs.Detail[p.GetPodIndex()].AddedToDB = false
				}
			}
		}
		return nil
	}
	return vdbstatus.Update(ctx, d.VRec.Client, d.Vdb, refreshInPlace)
}

// removeClientProxy will remove client proxy and related config map
func (d *DBRemoveSubclusterReconciler) removeClientProxy(ctx context.Context, sc *vapi.Subcluster) error {
	cmName := names.GenVProxyConfigMapName(d.Vdb, sc)
	vpName := names.GenVProxyName(d.Vdb, sc)
	vpDep := builder.BuildVProxyDeployment(vpName, d.Vdb, sc)
	cmDep := builder.BuildVProxyConfigMap(cmName, d.Vdb, sc)

	d.Log.Info("Deleting client proxy config map", "Name", cmName)
	err := d.VRec.GetClient().Delete(ctx, cmDep)
	if err != nil {
		return err
	}
	d.Log.Info("Delete deployment", "Name", vpName, "Size", vpDep.Spec.Replicas, "Image", vpDep.Spec.Template.Spec.Containers[0].Image)
	return deleteDep(ctx, d.VRec, vpDep, d.Vdb)
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
		// We use the FindStatefulSets() API to get subclusters that already exist.
		// We can only change the default subcluster to one of those.
		stss, err := scFinder.FindStatefulSets(ctx, iter.FindInVdb, vapi.MainCluster)
		if err != nil {
			return err
		}
		// If we don't find a service object we don't fail.  The attempt to
		// remove the default subcluster that we do later will fail.  That
		// provides a better error message than anything we do here.
		if len(stss.Items) > 0 {
			return d.changeDefaultSubcluster(ctx, stss.Items[0].Labels[vmeta.SubclusterNameLabel])
		}
	}
	return nil
}

// getDefaultSubcluster returns the name of the current default subcluster
func (d *DBRemoveSubclusterReconciler) getDefaultSubcluster(ctx context.Context) (string, error) {
	cmd := []string{
		"-tAc", "select subcluster_name from subclusters where is_default is true",
	}
	stdout, _, err := d.PRunner.ExecVSQL(ctx, d.ATPod.GetName(), names.ServerContainer, cmd...)
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
		"-c", fmt.Sprintf(`alter subcluster %q set default`, scName),
	}
	_, _, err := d.PRunner.ExecVSQL(ctx, d.ATPod.GetName(), names.ServerContainer, cmd...)
	return err
}
