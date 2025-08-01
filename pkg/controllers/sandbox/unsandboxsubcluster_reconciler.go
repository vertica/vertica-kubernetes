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

package sandbox

import (
	"context"
	"errors"

	"github.com/go-logr/logr"
	vutil "github.com/vertica/vcluster/vclusterops/util"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/unsandboxsc"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// UnsandboxSubclusterReconciler will unsandbox subclusters in the sandbox of a sandbox config map
type UnsandboxSubclusterReconciler struct {
	SRec *SandboxConfigMapReconciler
	Vdb  *vapi.VerticaDB
	Log  logr.Logger
	client.Client
	Dispatcher     vadmin.Dispatcher
	PFacts         *podfacts.PodFacts
	OriginalPFacts *podfacts.PodFacts
	ConfigMap      *corev1.ConfigMap
	InitiatorIP    string // The IP of the pod that we run vclusterOps from
	PRunner        cmds.PodRunner
}

func MakeUnsandboxSubclusterReconciler(r *SandboxConfigMapReconciler, vdb *vapi.VerticaDB, log logr.Logger,
	cli client.Client, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher,
	configMap *corev1.ConfigMap, prunner cmds.PodRunner) controllers.ReconcileActor {
	pfactsForMainCluster := pfacts.Copy(vapi.MainCluster)
	return &UnsandboxSubclusterReconciler{
		SRec:           r,
		Log:            log.WithName("UnsandboxSubclusterReconciler"),
		Vdb:            vdb,
		Client:         cli,
		Dispatcher:     dispatcher,
		PFacts:         &pfactsForMainCluster,
		OriginalPFacts: pfacts,
		ConfigMap:      configMap,
		PRunner:        prunner,
	}
}

// Reconcile will unsandbox subclusters, remove the sandbox config map, and update sandbox status of vdb
func (r *UnsandboxSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db
	if r.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly || !r.Vdb.IsEON() {
		return ctrl.Result{}, nil
	}

	// collect pod facts for main cluster
	if err := r.PFacts.Collect(ctx, r.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile sandbox status for the subclusters that are already unsandboxed
	if err := r.reconcileSandboxInfoInVdb(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile the sandbox config map if it expires
	err, deleted := r.reconcileSandboxConfigMap(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	// if we deleted the expired config map, we don't need to make unsandbox operation any more
	if deleted {
		return ctrl.Result{}, nil
	}

	// only execute unsandbox op when unsandbox trigger id and sandbox name are set
	if r.ConfigMap.Annotations[vmeta.SandboxControllerUnsandboxTriggerID] != "" &&
		r.ConfigMap.Data[vapi.SandboxNameKey] != "" {
		return r.unsandboxSubclusters(ctx)
	}
	return ctrl.Result{}, nil
}

// reconcileSandboxStatus will update vdb for the subclusters that are already unsandboxed
func (r *UnsandboxSubclusterReconciler) reconcileSandboxInfoInVdb(ctx context.Context) error {
	scSbInStatus := r.Vdb.GenSubclusterSandboxStatusMap()
	sbScMap := r.PFacts.FindUnsandboxedSubclustersStillInSandboxStatus(scSbInStatus)
	for sb, scs := range sbScMap {
		if sb == r.ConfigMap.Data[vapi.SandboxNameKey] {
			err := r.updateSandboxInfoInVdb(ctx, sb, scs)
			if err != nil {
				r.Log.Info("failed to update sandbox status", "sandbox", sb, "new subclusters", scs)
				return err
			}
			break
		}
	}

	return nil
}

// reconcileSandboxConfigMap will update/delete sandbox config map if it expires, this function will return
// an error and a boolean to indicate if sandbox config map is deleted
func (r *UnsandboxSubclusterReconciler) reconcileSandboxConfigMap(ctx context.Context) (error, bool) {
	if err := r.OriginalPFacts.Collect(ctx, r.Vdb); err != nil {
		return err, false
	}

	sbName := r.ConfigMap.Data[vapi.SandboxNameKey]
	cmName := r.ConfigMap.Name
	sb := r.Vdb.GetSandboxStatus(sbName)
	// if the sandbox doesn't have any subclusters, we delete the config map
	if r.OriginalPFacts.IsSandboxEmpty(sbName) && (sb == nil || len(sb.Subclusters) == 0) {
		err := r.Client.Delete(ctx, r.ConfigMap)
		if err != nil {
			r.Log.Error(err, "failed to delete expired sandbox config map", "configMapName", cmName)
			return err, false
		}
		r.Log.Info("deleted expired sandbox config map", "configMapName", cmName)
		return nil, true
	}
	if r.ConfigMap.Annotations[vmeta.SandboxControllerUnsandboxTriggerID] != "" {
		unsandboxSbScMap := r.Vdb.GenSandboxSubclusterMapForUnsandbox()
		_, found := unsandboxSbScMap[sbName]
		// if the subclusters in the sandbox does not need to be unsandboxed, we remove
		// unsandbox trigger ID from the config map
		if !found {
			chgs := vk8s.MetaChanges{
				AnnotationsToRemove: []string{vmeta.SandboxControllerUnsandboxTriggerID},
			}
			nm := names.GenNamespacedName(r.ConfigMap, cmName)
			_, err := vk8s.MetaUpdate(ctx, r.SRec.GetClient(), nm, r.ConfigMap, chgs)
			if err != nil {
				r.Log.Error(err, "failed to remove unsandbox trigger ID from sandbox config map", "configMapName", cmName)
				return err, false
			}
			r.Log.Info("Successfully removed unsandbox trigger ID from sandbox config map", "configMapName", cmName)
		}
	}

	return nil, false
}

// unsandboxSubclusters will unsandbox subclusters inside the sandbox from sandbox config map
func (r *UnsandboxSubclusterReconciler) unsandboxSubclusters(ctx context.Context) (ctrl.Result, error) {
	// find an initiator to call vclusterOps
	initiatorIP, ok := r.PFacts.FindFirstPrimaryUpPodIP()
	if ok {
		r.InitiatorIP = initiatorIP
	} else {
		r.Log.Info("Requeue because there are no UP nodes in main cluster to execute unsandbox operation")
		return ctrl.Result{Requeue: true}, nil
	}

	err := r.executeUnsandboxCommand(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}
	r.OriginalPFacts.Invalidate()

	return ctrl.Result{}, nil
}

// executeUnsandboxCommand will move subclusters from a sandbox to main cluster, update sandbox
// status, and delete the config map
func (r *UnsandboxSubclusterReconciler) executeUnsandboxCommand(ctx context.Context) error {
	unsandboxSbScMap := r.Vdb.GenSandboxSubclusterMapForUnsandbox()
	sbName := r.ConfigMap.Data[vapi.SandboxNameKey]
	scs, found := unsandboxSbScMap[sbName]
	if !found {
		r.Log.Info("Ignore the config map because the sandbox inside it does not need to be unsandboxed")
		return nil
	}
	succeedScs := []string{}
	for _, sc := range scs {
		err := r.unsandboxSubcluster(ctx, sc)
		if err != nil {
			// when failed to unsandbox a subcluster, update sandbox status and return error
			return errors.Join(err, r.updateSandboxInfoInVdb(ctx, sbName, succeedScs))
		}
		succeedScs = append(succeedScs, sc)
	}
	err := r.updateSandboxInfoInVdb(ctx, sbName, succeedScs)
	if err != nil {
		// when failed to update sandbox status, we will still try to process the sandbox config map
		return errors.Join(err, r.processConfigMap(ctx))
	}
	return r.processConfigMap(ctx)
}

// processConfigMap will delete the sandbox config map if the sandbox doesn't contain any subclusters,
// otherwise it will remove unsandbox trigger ID in that config map
func (r *UnsandboxSubclusterReconciler) processConfigMap(ctx context.Context) error {
	cmName := r.ConfigMap.Name
	sb := r.Vdb.GetSandboxStatus(r.ConfigMap.Data[vapi.SandboxNameKey])
	if sb == nil || len(sb.Subclusters) == 0 {
		err := r.Client.Delete(ctx, r.ConfigMap)
		if err != nil {
			r.Log.Error(err, "failed to delete sandbox config map", "configMapName", cmName)
			return err
		}
		r.Log.Info("Successfully deleted sandbox config map", "configMapName", cmName)
		return nil
	}

	chgs := vk8s.MetaChanges{
		AnnotationsToRemove: []string{vmeta.SandboxControllerUnsandboxTriggerID},
	}
	nm := names.GenNamespacedName(r.ConfigMap, cmName)
	_, err := vk8s.MetaUpdate(ctx, r.SRec.GetClient(), nm, r.ConfigMap, chgs)
	if err != nil {
		r.Log.Error(err, "failed to remove unsandbox trigger ID from sandbox config map", "configMapName", cmName)
		return err
	}
	r.Log.Info("Successfully removed unsandbox trigger ID from sandbox config map", "configMapName", cmName)

	return nil
}

// unsandboxSubcluster will move subclusters from a sandbox to main cluster by calling vclusterOps
func (r *UnsandboxSubclusterReconciler) unsandboxSubcluster(ctx context.Context, scName string) error {
	if err := r.OriginalPFacts.Collect(ctx, r.Vdb); err != nil {
		return err
	}

	// nodes' names and addresses in the subcluster to unsandbox. These names and addresses
	// are the latest ones in the database, and vclusterOps will compare them with the ones in catalog
	// of main cluster. If vclusterOps find catalog of main cluster has stale node addresses,
	// it will use the correct addresses in this map to do a re-ip before unsandboxing.
	nodeNameAddressMap := r.OriginalPFacts.FindNodeNameAndAddressInSubcluster(scName)

	// remove startup.json in pod since vcluster unsandbox needs to poll node down.
	// With that json file, the container will restart vertica automatically and fail
	// vcluster unsandbox
	err := r.OriginalPFacts.RemoveStartupFileInSubclusterPods(ctx, scName, "removed startup.json before unsandboxing")
	if err != nil {
		return err
	}

	sbInitiator, ok := r.OriginalPFacts.GetInitiatorIPInSB(r.ConfigMap.Data[vapi.SandboxNameKey], scName)
	if !ok {
		r.Log.Info("Cannot find initiator in sandbox. The sandbox may only have one subcluster",
			"sandboxName", r.ConfigMap.Data[vapi.SandboxNameKey])
	}
	r.SRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.UnsandboxSubclusterStart,
		"Starting unsandbox subcluster %q", scName)
	err = r.Dispatcher.UnsandboxSubcluster(ctx,
		unsandboxsc.WithInitiator(r.InitiatorIP),
		unsandboxsc.WithSBInitiator(sbInitiator),
		unsandboxsc.WithSubcluster(scName),
		// vclusterOps needs correct node names and addresses to do re-ip
		unsandboxsc.WithNodeNameAddressMap(nodeNameAddressMap),
	)
	if err != nil {
		r.SRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.UnsandboxSubclusterFailed,
			"Failed to unsandbox subcluster %q", scName)
		return err
	}
	r.SRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.UnsandboxSubclusterSucceeded,
		"Successfully unsandboxed subcluster %q", scName)
	return nil
}

// updateSandboxInfoInVdb will update the sandbox status in vdb
func (r *UnsandboxSubclusterReconciler) updateSandboxInfoInVdb(ctx context.Context, sbName string, unsandboxedScNames []string) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// update the sandbox's subclusters in sandbox status
		for i := len(vdbChg.Status.Sandboxes) - 1; i >= 0; i-- {
			if vdbChg.Status.Sandboxes[i].Name != sbName {
				continue
			}
			vdbChg.Status.Sandboxes[i].Subclusters = vutil.SliceDiff(vdbChg.Status.Sandboxes[i].Subclusters, unsandboxedScNames)
			if len(vdbChg.Status.Sandboxes[i].Subclusters) == 0 {
				vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes[:i], vdbChg.Status.Sandboxes[i+1:]...)
			}
			break
		}

		return nil
	}

	return vdbstatus.Update(ctx, r.Client, r.Vdb, updateStatus)
}
