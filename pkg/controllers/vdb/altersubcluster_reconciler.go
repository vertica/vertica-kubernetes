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
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, configMap *corev1.ConfigMap) controllers.ReconcileActor {
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

	ctrlResult, scs, err := a.findSubclustersToAlter()
	if err != nil || len(scs) == 0 {
		return ctrlResult, err
	}

	return a.alterSubclusters(ctx, scs)
}

// findSubclustersToAlter returns a list of main/sandbox subclusters whose type needs to be changed
func (a *AlterSubclusterTypeReconciler) findSubclustersToAlter() (ctrl.Result, []string, error) {
	scs := []string{}
	scMap := a.Vdb.GenSubclusterMap()
	seen := make(map[string]bool)
	for scName, sc := range scMap {
		if seen[sc.Name] {
			continue
		}

		// find the subcluster whose type is different to podfacts (which reads the database)
		pf, ok := a.PFacts.FindFirstUpPod(false, scName)
		if !ok {
			return ctrl.Result{Requeue: true}, scs, nil
		}

		// check if the subcluster is in sandbox
		if pf.GetSandbox() != vapi.MainCluster {
			sbscs, err := a.findSandboxSubclustersToAlter(pf.GetSandbox(), &seen)
			if err != nil {
				return ctrl.Result{}, scs, err
			}
			scs = append(scs, sbscs...)
			continue
		}

		// check in main cluster
		if sc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			scs = append(scs, sc.Name)
		}
		seen[scName] = true
	}
	return ctrl.Result{}, scs, nil
}

// findSandboxSubclustersToAlter finds the sandbox subclusters that need to be altered
func (a *AlterSubclusterTypeReconciler) findSandboxSubclustersToAlter(sbName string, seen *map[string]bool) ([]string, error) {
	sbscs := []string{}
	sb := a.Vdb.GetSandbox(sbName)
	if sb == nil {
		return sbscs, fmt.Errorf("could not find sandbox %s", sbName)
	}
	for _, sbsc := range sb.Subclusters {
		if (*seen)[sbsc.Name] {
			continue
		}
		pf, ok := a.PFacts.FindFirstUpPod(false, sbsc.Name)
		if !ok {
			return sbscs, fmt.Errorf("could not find pod for sandbox subcluster %s", sbsc.Name)
		}
		if sbsc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sbsc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			sbscs = append(sbscs, sbsc.Name)
		}
		(*seen)[sbsc.Name] = true
	}
	return sbscs, nil
}

func (a *AlterSubclusterTypeReconciler) alterSubclusters(ctx context.Context, scs []string) (ctrl.Result, error) {
	for _, scName := range scs {
		initiatorIP, requeue := a.getInitiatorIP()
		if requeue {
			return ctrl.Result{Requeue: requeue}, nil
		}
		_, ok := a.PFacts.FindFirstUpPod(false, scName)
		if !ok {
			return ctrl.Result{Requeue: requeue}, nil
		}
		sc := a.Vdb.GenSubclusterMap()[scName]
		if sc == nil {
			return ctrl.Result{}, fmt.Errorf("could not find subcluster %s", scName)
		}
		a.Log.Info("Altering subcluster type", "subcluster", sc.Name, "initiatorIP", initiatorIP)
		err := a.alterSubclusterType(ctx, sc, initiatorIP)
		if err != nil {
			return ctrl.Result{}, err
		}
		a.Log.Info("Alter subcluster type completed", "subcluster", sc.Name)
	}

	return a.removeTriggerIDFromConfigMap(ctx)
}

// alterSubclusterType changes the given subcluster's type
func (a *AlterSubclusterTypeReconciler) alterSubclusterType(ctx context.Context, sc *vapi.Subcluster,
	initiatorIP string) error {
	scType := vapi.SecondarySubcluster
	newType := vapi.PrimarySubcluster

	// check db is_primary
	pf, ok := a.PFacts.FindFirstUpPod(false, sc.Name)
	if ok && pf.GetIsPrimary() {
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
		return err
	}

	pf.SetIsPrimary(newType == vapi.PrimarySubcluster)
	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.AlterSubclusterSucceeded,
		"Successfully altered the type of subcluster %q to %q", sc.Name, newType)
	return nil
}

// removeTriggerIDFromConfigMap will remove alter sandbox type trigger ID in that config map
func (a *AlterSubclusterTypeReconciler) removeTriggerIDFromConfigMap(ctx context.Context) (ctrl.Result, error) {
	if a.ConfigMap == nil {
		return ctrl.Result{}, nil
	}
	cmName := a.ConfigMap.Name
	chgs := vk8s.MetaChanges{
		AnnotationsToRemove: []string{vmeta.SandboxControllerAlterSubclusterTypeTriggerID},
	}
	nm := names.GenNamespacedName(a.ConfigMap, cmName)
	_, err := vk8s.MetaUpdate(ctx, a.VRec.GetClient(), nm, a.ConfigMap, chgs)
	if err != nil {
		a.Log.Error(err, "failed to remove alter subcluster type trigger ID from sandbox config map", "configMapName", cmName)
		return ctrl.Result{}, err
	}
	a.Log.Info("Successfully removed alter subcluster type trigger ID from sandbox config map", "configMapName", cmName)

	return ctrl.Result{}, nil
}

// getInitiatorIP returns the initiator ip that will be used for
// alterSubclusterType
func (a *AlterSubclusterTypeReconciler) getInitiatorIP() (string, bool) {
	initiator, ok := a.PFacts.FindFirstPodSorted(func(v *podfacts.PodFact) bool {
		return v.GetIsPrimary() && v.GetUpNode()
	})
	if !ok {
		a.Log.Info("No Up nodes found. Requeue reconciliation.")
		return "", true
	}
	return initiator.GetPodIP(), false
}
