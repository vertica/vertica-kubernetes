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
	"slices"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/altersc"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AlterSubclusterTypeReconciler will change a subcluster type
type AlterSubclusterTypeReconciler struct {
	VRec       config.ReconcilerInterface
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts     *podfacts.PodFacts
	Dispatcher vadmin.Dispatcher
	ConfigMap  *corev1.ConfigMap
}

// MakeAlterSubclusterTypeReconciler will build a AlterSubclusterTypeReconciler object
func MakeAlterSubclusterTypeReconciler(vdbrecon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher,
	configMap *corev1.ConfigMap) controllers.ReconcileActor {
	return &AlterSubclusterTypeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("AlterSubclusterTypeReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
		ConfigMap:  configMap,
	}
}

func (a *AlterSubclusterTypeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	var err error
	var scs []string
	// find sandbox subclusters to alter
	if a.ConfigMap != nil {
		// only execute alter subcluster type op when alter sandbox trigger id is set
		if a.ConfigMap.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID] == "" {
			return ctrl.Result{}, nil
		}
		scs, err = a.findSandboxSubclustersToAlter(ctx)
	} else {
		scs, err = a.findMainSubclustersToAlter()
	}
	if err == nil && len(scs) == 0 && a.ConfigMap != nil &&
		a.ConfigMap.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID] != "" {
		return ctrl.Result{}, a.removeTriggerIDFromConfigMap(ctx)
	}
	if err != nil || len(scs) == 0 {
		return ctrl.Result{}, err
	}

	initiatorIP, requeue := a.getInitiatorIP(scs)
	if requeue {
		a.Log.Info("Requeue alterSubclusters: could not find initiatorIP")
		return ctrl.Result{Requeue: requeue}, nil
	}

	return a.alterSubclusters(ctx, scs, initiatorIP)
}

// findMainSubclustersToAlter returns a list of main subclusters whose type needs to be changed
func (a *AlterSubclusterTypeReconciler) findMainSubclustersToAlter() ([]string, error) {
	scs := []string{}
	scMap := a.Vdb.GenSubclusterMap()
	scSbMap := a.Vdb.GenSubclusterSandboxStatusMap()
	for scName, sc := range scMap {
		if sc == nil {
			return nil, fmt.Errorf("could not find subcluster %s", scName)
		}
		if sb, ok := scSbMap[scName]; ok && sb != vapi.MainCluster {
			// skip sandbox subclusters
			continue
		}
		// find the subcluster whose type is different to podfacts (which reads the database)
		pf, ok := a.PFacts.FindFirstUpPod(false, scName)
		if !ok {
			a.Log.Info("skipping subcluster, no pods are up", "subcluster", scName)
			continue
		}
		// check if the subcluster type is different to podfacts
		if sc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			a.Log.Info("Found subcluster to alter", "subcluster", sc.Name,
				"subcluster type", sc.Type, "podfacts is primary", pf.GetIsPrimary())
			scs = append(scs, sc.Name)
		}
	}

	return scs, nil
}

// findSandboxSubclustersToAlter finds the sandbox subclusters that need to be altered
// sbpfacts is for unit test purposes
func (a *AlterSubclusterTypeReconciler) findSandboxSubclustersToAlter(ctx context.Context) ([]string, error) {
	sbName := a.PFacts.SandboxName
	sbscs := []string{}
	sb := a.Vdb.GetSandbox(sbName)
	if sb == nil {
		return sbscs, fmt.Errorf("could not find sandbox %s", sbName)
	}

	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return sbscs, fmt.Errorf("failed to collect pod facts for sandbox %s: %w", sbName, err)
	}
	for _, sbsc := range sb.Subclusters {
		pf, ok := a.PFacts.FindFirstUpPod(false, sbsc.Name)
		if !ok {
			a.Log.Info("skipping sandbox subcluster, no pods are up", "subcluster", sbsc.Name)
			continue
		}
		// check if the sandbox subcluster type is different to podfacts
		if sbsc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sbsc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			a.Log.Info("Found sandbox subcluster to alter", "subcluster", sbsc.Name,
				"sandbox subcluster type", sbsc.Type, "podfacts is primary", pf.GetIsPrimary())
			sbscs = append(sbscs, sbsc.Name)
		}
	}

	return sbscs, nil
}

func (a *AlterSubclusterTypeReconciler) alterSubclusters(ctx context.Context, scs []string, initiatorIP string) (ctrl.Result, error) {
	for _, scName := range scs {
		sc := a.Vdb.GenSubclusterMap()[scName]
		if sc == nil {
			return ctrl.Result{}, fmt.Errorf("could not find subcluster %s", scName)
		}
		a.Log.Info("Altering subcluster type", "subcluster", sc.Name, "initiatorIP", initiatorIP)
		ctrlResult, err := a.alterSubclusterType(ctx, sc, initiatorIP)
		if err != nil {
			return ctrlResult, err
		}
		a.Log.Info("Alter subcluster type completed", "subcluster", sc.Name)
	}
	return ctrl.Result{}, a.removeTriggerIDFromConfigMap(ctx)
}

// alterSubclusterType changes the given subcluster's type
func (a *AlterSubclusterTypeReconciler) alterSubclusterType(ctx context.Context, sc *vapi.Subcluster,
	initiatorIP string) (ctrl.Result, error) {
	scType := vapi.SecondarySubcluster
	newType := vapi.PrimarySubcluster

	pf, ok := a.PFacts.FindFirstUpPod(false, sc.Name)
	if !ok {
		a.Log.Info("Requeue alterSubclusterType: could not find pod for subcluster", "subcluster", sc.Name)
		return ctrl.Result{Requeue: true}, nil
	}

	// check db is_primary
	if pf.GetIsPrimary() {
		scType = vapi.PrimarySubcluster
		newType = vapi.SecondarySubcluster
	}
	a.Log.Info("Starting alter subcluster", "subcluster", sc.Name, "from", scType, "to", newType)
	err := a.Dispatcher.AlterSubclusterType(ctx,
		altersc.WithInitiator(initiatorIP),
		altersc.WithSubcluster(sc.Name),
		altersc.WithSubclusterType(scType),
		altersc.WithSandbox(a.PFacts.GetSandboxName()),
	)
	if err != nil {
		a.VRec.Eventf(a.Vdb, corev1.EventTypeWarning, events.AlterSubclusterFailed,
			"Failed to alter the type of subcluster %q to %q", sc.Name, newType)
		return ctrl.Result{}, err
	}

	a.PFacts.Invalidate()
	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.AlterSubclusterSucceeded,
		"Successfully altered the type of subcluster %q to %q", sc.Name, newType)
	return ctrl.Result{}, nil
}

// removeTriggerIDFromConfigMap will remove alter sandbox type trigger ID in that config map
func (a *AlterSubclusterTypeReconciler) removeTriggerIDFromConfigMap(ctx context.Context) error {
	if a.ConfigMap == nil {
		return nil
	}
	if a.ConfigMap.Annotations[vmeta.SandboxControllerAlterSubclusterTypeTriggerID] == "" ||
		a.ConfigMap.Data[vapi.SandboxNameKey] == "" {
		return nil
	}
	cmName := a.ConfigMap.Name
	chgs := vk8s.MetaChanges{
		AnnotationsToRemove: []string{vmeta.SandboxControllerAlterSubclusterTypeTriggerID},
	}
	nm := names.GenNamespacedName(a.ConfigMap, cmName)
	_, err := vk8s.MetaUpdate(ctx, a.VRec.GetClient(), nm, a.ConfigMap, chgs)
	if err != nil {
		a.Log.Error(err, "failed to remove alter subcluster type trigger ID from sandbox config map", "configMapName", cmName)
		return err
	}
	a.Log.Info("Successfully removed alter subcluster type trigger ID from sandbox config map", "configMapName", cmName)

	return nil
}

// getInitiatorIP returns the initiator ip not to be demoted that will be used for
// alterSubclusterType
func (a *AlterSubclusterTypeReconciler) getInitiatorIP(scs []string) (string, bool) {
	initiator, ok := a.PFacts.FindFirstPodSorted(func(v *podfacts.PodFact) bool {
		return !slices.Contains(scs, v.GetSubclusterName()) && v.GetIsPrimary() && v.GetUpNode()
	})
	if !ok {
		a.Log.Info("No Up nodes found. Requeue reconciliation.")
		return "", true
	}
	a.Log.Info("DEBUG:Initiator ip found", "initiatorIP", initiator.GetPodIP())
	return initiator.GetPodIP(), false
}
