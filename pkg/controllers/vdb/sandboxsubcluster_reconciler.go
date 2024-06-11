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
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/sandboxsc"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SandboxSubclusterReconciler will add subclusters to sandboxes
type SandboxSubclusterReconciler struct {
	VRec         *VerticaDBReconciler
	Log          logr.Logger
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on
	PFacts       *podfacts.PodFacts
	InitiatorIPs map[string]string // IPs from main cluster and sandboxes that should be passed down to vcluster
	Dispatcher   vadmin.Dispatcher
	client.Client
}

// MakeSandboxSubclusterReconciler will build a SandboxSubclusterReconciler object
func MakeSandboxSubclusterReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger, vdb *vapi.VerticaDB,
	pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, cli client.Client) controllers.ReconcileActor {
	return &SandboxSubclusterReconciler{
		VRec:         vdbrecon,
		Log:          log.WithName("SandboxSubclusterReconciler"),
		Vdb:          vdb,
		InitiatorIPs: make(map[string]string),
		PFacts:       pfacts,
		Dispatcher:   dispatcher,
		Client:       cli,
	}
}

// Reconcile will add subclusters to sandboxes if we found any qualified subclusters
func (s *SandboxSubclusterReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// no-op for ScheduleOnly init policy or enterprise db or no sandboxes
	if s.Vdb.Spec.InitPolicy == vapi.CommunalInitPolicyScheduleOnly ||
		!s.Vdb.IsEON() || len(s.Vdb.Spec.Sandboxes) == 0 {
		return ctrl.Result{}, nil
	}

	// We need to collect pod facts for finding qualified subclusters
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile sandbox status for the subclusters that are already sandboxed
	if err := s.reconcileSandboxStatus(ctx); err != nil {
		return ctrl.Result{}, err
	}

	// reconcile sandbox config maps for the existing sandboxes
	if err := s.reconcileSandboxConfigMaps(ctx); err != nil {
		return ctrl.Result{}, err
	}

	return s.sandboxSubclusters(ctx)
}

// reconcileSandboxStatus will update sandbox status for the subclusters that are already sandboxed
func (s *SandboxSubclusterReconciler) reconcileSandboxStatus(ctx context.Context) error {
	sbScMap := make(map[string][]string)
	seenScs := make(map[string]any)
	for _, v := range s.PFacts.Detail {
		if _, ok := seenScs[v.GetSubclusterName()]; ok {
			continue
		}
		if v.GetSandbox() != vapi.MainCluster {
			sbScMap[v.GetSandbox()] = append(sbScMap[v.GetSandbox()], v.GetSubclusterName())
		}
		seenScs[v.GetSubclusterName()] = struct{}{}
	}
	if len(sbScMap) > 0 {
		return s.updateSandboxStatus(ctx, sbScMap)
	}

	return nil
}

// reconcileSandboxConfigMaps will create/update sandbox config maps for the existing sandboxes
func (s *SandboxSubclusterReconciler) reconcileSandboxConfigMaps(ctx context.Context) error {
	for _, sb := range s.Vdb.Status.Sandboxes {
		err := s.checkSandboxConfigMap(ctx, sb.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

// sandboxSubclusters will add subclusters to their sandboxes defined in the vdb
func (s *SandboxSubclusterReconciler) sandboxSubclusters(ctx context.Context) (ctrl.Result, error) {
	// find qualified subclusters with their sandboxes
	scSbMap, allNodesUp := s.fetchSubclustersWithSandboxes()
	if !allNodesUp {
		s.Log.Info("Requeue because we need all nodes in target subclusters are UP")
		return ctrl.Result{Requeue: true}, nil
	}
	if len(scSbMap) == 0 {
		s.Log.Info("No subclusters need to be sandboxed")
		return ctrl.Result{}, nil
	}

	// find an initiator from the main cluster that we can use to pass down to
	// vclusterOps.
	initiator, ok := s.PFacts.FindFirstPodSorted(func(v *podfacts.PodFact) bool {
		return v.GetSandbox() == vapi.MainCluster && v.GetIsPrimary() && v.GetUpNode()
	})
	if ok {
		s.InitiatorIPs[vapi.MainCluster] = initiator.GetPodIP()
	} else {
		s.Log.Info("Requeue because there are no UP nodes in main cluster to execute sandbox operation")
		return ctrl.Result{Requeue: true}, nil
	}

	res, err := s.executeSandboxCommand(ctx, scSbMap)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}
	s.PFacts.Invalidate()

	return ctrl.Result{}, nil
}

// executeSandboxCommand will call sandbox API in vclusterOps, create/update sandbox config maps,
// and update sandbox status in vdb
func (s *SandboxSubclusterReconciler) executeSandboxCommand(ctx context.Context, scSbMap map[string]string) (ctrl.Result, error) {
	seenSandboxes := make(map[string]any)

	// We can simply loop over the scSbMap and sandbox each subcluster. However,
	// we want to sandbox in a determinstic order because the first subcluster
	// in a sandbox is the primary.
	for i := range s.Vdb.Spec.Sandboxes {
		vdbSb := &s.Vdb.Spec.Sandboxes[i]
		for j := range vdbSb.Subclusters {
			sc := vdbSb.Subclusters[j].Name
			sb, found := scSbMap[sc]
			if !found {
				// assume it is already sandboxed
				continue
			}
			// The first subcluster in a sandbox turns into a primary. Set
			// state in the vdb to indicate that.
			if j == 0 {
				_, err := vk8s.UpdateVDBWithRetry(ctx, s.VRec, s.Vdb, func() (bool, error) {
					scMap := s.Vdb.GenSubclusterMap()
					vdbSc, found := scMap[sc]
					if !found {
						return false, fmt.Errorf("subcluster %q missing in vdb %q", sc, s.Vdb.Name)
					}
					vdbSc.Type = vapi.SandboxPrimarySubcluster
					return true, nil
				})
				if err != nil {
					return ctrl.Result{}, err
				}
			}
			res, err := s.sandboxSubcluster(ctx, sc, sb)
			if verrors.IsReconcileAborted(res, err) {
				return res, err
			}
			seenSandboxes[sb] = struct{}{}

			// Always update status as we go. When sandboxing two subclusters in
			// the same sandbox, the second subcluster depends on the status
			// update from the first subcluster in order to properly sandbox.
			err = s.addSandboxedSubclusterToStatus(ctx, sc, sb)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
	}

	// create/update a sandbox config map
	for sb := range seenSandboxes {
		err := s.checkSandboxConfigMap(ctx, sb)
		if err != nil {
			// when creating/updating sandbox config map failed, update sandbox status and return error
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// findInitiatorIPs returns the IPs to pass down to vclusterops as the initiator
// nodes. The number of IPs could be one or two. If two, they are separated with
// a comma.
func (s *SandboxSubclusterReconciler) findInitiatorIPs(ctx context.Context, sandbox string) ([]string, ctrl.Result, error) {
	// We already have an IP of an up pod from the main cluster. If we are
	// adding to an existing sandbox, then we need to find an IP from a pod in
	// that sandbox too.
	_, found := s.InitiatorIPs[sandbox]
	if !found {
		pfs := s.PFacts.Copy(sandbox)
		if err := pfs.Collect(ctx, s.Vdb); err != nil {
			return nil, ctrl.Result{}, err
		}
		// If this is the first pod in the sandbox, then only an ip from the
		// main cluster is needed.
		if len(pfs.Detail) == 0 {
			ips := []string{s.InitiatorIPs[vapi.MainCluster]}
			s.Log.Info("detected first subcluster added to a new sandbox", "initiatorIPs", ips)
			return ips, ctrl.Result{}, nil
		}
		pf, found := pfs.FindFirstPodSorted(func(v *podfacts.PodFact) bool {
			return v.GetUpNode() && v.GetIsPrimary()
		})
		if !found {
			s.Log.Info("Requeue because there are no UP nodes in the target sandbox", "sandbox", sandbox)
			return nil, ctrl.Result{Requeue: true}, nil
		}
		s.InitiatorIPs[sandbox] = pf.GetPodIP()
	}
	ips := []string{s.InitiatorIPs[vapi.MainCluster], s.InitiatorIPs[sandbox]}
	s.Log.Info("found two initiator IPs", "main", ips[0], "sandbox", ips[1])
	return ips, ctrl.Result{}, nil
}

// checkSandboxConfigMap will create or update a sandbox config map if needed
func (s *SandboxSubclusterReconciler) checkSandboxConfigMap(ctx context.Context, sandbox string) error {
	nm := names.GenSandboxConfigMapName(s.Vdb, sandbox)
	curCM := &corev1.ConfigMap{}
	newCM := builder.BuildSandboxConfigMap(nm, s.Vdb, sandbox)
	err := s.Client.Get(ctx, nm, curCM)
	if err != nil && kerrors.IsNotFound(err) {
		s.Log.Info("Creating sandbox config map", "Name", nm)
		return s.Client.Create(ctx, newCM)
	}
	if s.updateSandboxConfigMapFields(curCM, newCM) {
		s.Log.Info("Updating sandbox config map", "Name", nm)
		return s.Client.Update(ctx, newCM)
	}
	s.Log.Info("Found an existing sandbox config map with correct content, skip updating it", "Name", nm)
	return nil
}

// updateSandboxConfigMapFields checks if we need to update the content of a config map,
// if so, we will update the content of that config map and return true
func (s *SandboxSubclusterReconciler) updateSandboxConfigMapFields(curCM, newCM *corev1.ConfigMap) bool {
	updated := false
	// exclude sandbox controller upgrade & unsandbox trigger ID from the annotations
	// because vdb controller will set this in current config map, and the new
	// config map cannot get it
	upgradeTriggerID, hasUpgradeTriggerID := curCM.Annotations[vmeta.SandboxControllerUpgradeTriggerID]
	if hasUpgradeTriggerID {
		delete(curCM.Annotations, vmeta.SandboxControllerUpgradeTriggerID)
	}
	delete(newCM.Annotations, vmeta.SandboxControllerUpgradeTriggerID)
	unsandboxTriggerID, hasUnsandboxTriggerID := curCM.Annotations[vmeta.SandboxControllerUnsandboxTriggerID]
	if hasUnsandboxTriggerID {
		delete(curCM.Annotations, vmeta.SandboxControllerUnsandboxTriggerID)
	}
	delete(newCM.Annotations, vmeta.SandboxControllerUnsandboxTriggerID)

	// exclude version annotation because vdb controller can set a different
	// vertica version annotation for a sandbox in current config map
	version, hasVersion := curCM.Annotations[vmeta.VersionAnnotation]
	if hasVersion {
		delete(curCM.Annotations, vmeta.VersionAnnotation)
	}
	delete(newCM.Annotations, vmeta.VersionAnnotation)

	if stringMapDiffer(curCM.ObjectMeta.Annotations, newCM.ObjectMeta.Annotations) {
		updated = true
		curCM.ObjectMeta.Annotations = newCM.ObjectMeta.Annotations
	}

	// add sandbox controller upgrade & unsandbox trigger ID back to the annotations
	if hasUpgradeTriggerID {
		curCM.Annotations[vmeta.SandboxControllerUpgradeTriggerID] = upgradeTriggerID
	}
	if hasUnsandboxTriggerID {
		curCM.Annotations[vmeta.SandboxControllerUnsandboxTriggerID] = unsandboxTriggerID
	}
	// add vertica version back to the annotations
	if hasVersion {
		curCM.Annotations[vmeta.VersionAnnotation] = version
	}

	if stringMapDiffer(curCM.ObjectMeta.Labels, newCM.ObjectMeta.Labels) {
		updated = true
		curCM.ObjectMeta.Labels = newCM.ObjectMeta.Labels
	}
	if !reflect.DeepEqual(curCM.ObjectMeta.OwnerReferences, newCM.ObjectMeta.OwnerReferences) {
		updated = true
		curCM.ObjectMeta.OwnerReferences = newCM.ObjectMeta.OwnerReferences
	}
	if !reflect.DeepEqual(curCM.TypeMeta, newCM.TypeMeta) {
		updated = true
		curCM.TypeMeta = newCM.TypeMeta
	}
	if !*curCM.Immutable && stringMapDiffer(curCM.Data, newCM.Data) {
		updated = true
		curCM.Data = newCM.Data
	}
	if *curCM.Immutable != *newCM.Immutable {
		updated = true
		curCM.Immutable = newCM.Immutable
	}

	return updated
}

// fetchSubclustersWithSandboxes will return the qualified subclusters with their sandboxes
func (s *SandboxSubclusterReconciler) fetchSubclustersWithSandboxes() (map[string]string, bool) {
	vdbScSbMap := s.Vdb.GenSubclusterSandboxMap()
	targetScSbMap := make(map[string]string)
	for _, v := range s.PFacts.Detail {
		sb, ok := vdbScSbMap[v.GetSubclusterName()]
		// skip the pod in the subcluster that doesn't need to be sandboxed
		if !ok {
			continue
		}
		// skip the pod in the subcluster that already in the target sandbox
		if sb == v.GetSandbox() {
			continue
		}
		// the pod to be added in a sandbox should have a running node
		if !v.GetUpNode() {
			return targetScSbMap, false
		}
		targetScSbMap[v.GetSubclusterName()] = sb
	}
	return targetScSbMap, true
}

// sandboxSubcluster will add a subcluster to a sandbox by calling vclusterOps
func (s *SandboxSubclusterReconciler) sandboxSubcluster(ctx context.Context, subcluster, sandbox string) (ctrl.Result, error) {
	initiatorIPs, res, err := s.findInitiatorIPs(ctx, sandbox)
	if verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// nodes' names and addresses in the subcluster to sandbox. These names and addresses
	// are the latest ones in the database, and vclusterOps will compare them with the ones in catalog
	// of the target sandbox. If vclusterOps find catalog of the target sandbox has stale node addresses,
	// it will use the correct addresses in this map to do a re-ip before sandboxing.
	nodeNameAddressMap := s.PFacts.FindNodeNameAndAddressInSubcluster(subcluster)

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SandboxSubclusterStart,
		"Starting add subcluster %q to sandbox %q", subcluster, sandbox)
	err = s.Dispatcher.SandboxSubcluster(ctx,
		sandboxsc.WithInitiators(initiatorIPs),
		sandboxsc.WithSubcluster(subcluster),
		sandboxsc.WithSandbox(sandbox),
		// vclusterOps needs an up host of the target sandbox to do re-ip
		sandboxsc.WithUpHostInSandbox(s.InitiatorIPs[sandbox]),
		// vclusterOps needs correct node names and addresses to do re-ip
		sandboxsc.WithNodeNameAddressMap(nodeNameAddressMap),
	)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.SandboxSubclusterFailed,
			"Failed to add subcluster %q to sandbox %q", subcluster, sandbox)
		return ctrl.Result{}, err
	}
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SandboxSubclusterSucceeded,
		"Successfully added subcluster %q to sandbox %q", subcluster, sandbox)
	return ctrl.Result{}, nil
}

// updateSandboxStatus will update sandbox status in vdb. This is a bulk update
// and can handle multiple subclusters at once.
func (s *SandboxSubclusterReconciler) updateSandboxStatus(ctx context.Context, originalSbScMap map[string][]string) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// make a copy of originalSbScMap since we will modify the map, and
		// we want to have a good map in next retry
		sbScMap := make(map[string][]string, len(originalSbScMap))
		for sb, scs := range originalSbScMap {
			sbScMap[sb] = make([]string, len(scs))
			copy(sbScMap[sb], scs)
		}

		// for existing sandboxes, update their subclusters in sandbox status
		for i, sbStatus := range vdbChg.Status.Sandboxes {
			scs, ok := sbScMap[sbStatus.Name]
			if ok {
				vdbChg.Status.Sandboxes[i].Subclusters = append(vdbChg.Status.Sandboxes[i].Subclusters, scs...)
				delete(sbScMap, sbStatus.Name)
			}
		}

		// for new sandboxes, append them in sandbox status
		for sb, scs := range sbScMap {
			newStatus := vapi.SandboxStatus{Name: sb, Subclusters: scs}
			vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes, newStatus)
		}
		return nil
	}

	return vdbstatus.Update(ctx, s.Client, s.Vdb, updateStatus)
}

// addSandboxedSubclusterToStatus will add a single subcluster to the sandbox status.
func (s *SandboxSubclusterReconciler) addSandboxedSubclusterToStatus(ctx context.Context, subcluster, sandbox string) error {
	updateStatus := func(vdbChg *vapi.VerticaDB) error {
		// for existing sandboxes, update their subclusters in sandbox status
		for i := range vdbChg.Status.Sandboxes {
			if vdbChg.Status.Sandboxes[i].Name == sandbox {
				vdbChg.Status.Sandboxes[i].Subclusters = append(vdbChg.Status.Sandboxes[i].Subclusters, subcluster)
				return nil
			}
		}

		// If we get here, we didn't find the sandbox. So we are sandboxing the
		// first subcluster in a sandbox.
		newStatus := vapi.SandboxStatus{Name: sandbox, Subclusters: []string{subcluster}}
		vdbChg.Status.Sandboxes = append(vdbChg.Status.Sandboxes, newStatus)
		return nil
	}
	return vdbstatus.Update(ctx, s.Client, s.Vdb, updateStatus)
}
