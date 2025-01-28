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
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/altersc"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// AlterSubclusterTypeReconciler will change a subcluster type
type AlterSubclusterTypeReconciler struct {
	VRec       *VerticaDBReconciler
	Log        logr.Logger
	Vdb        *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts     *podfacts.PodFacts
	Dispatcher vadmin.Dispatcher
}

// MakeAlterSubclusterTypeReconciler will build a AlterSubclusterTypeReconciler object
func MakeAlterSubclusterTypeReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &AlterSubclusterTypeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("AlterSubclusterTypeReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
	}
}

func (a *AlterSubclusterTypeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if a.PFacts.SandboxName == vapi.MainCluster {
		return ctrl.Result{}, nil
	}

	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	scs, err := a.findSandboxSubclustersToAlter()
	if err != nil || len(scs) == 0 {
		return ctrl.Result{}, err
	}

	return a.alterSubclusters(ctx, scs)
}

// findSandboxSubclustersToAlter returns a list of subclusters whose type needs to be changed
func (a *AlterSubclusterTypeReconciler) findSandboxSubclustersToAlter() ([]*vapi.Subcluster, error) {
	scs := []*vapi.Subcluster{}
	sb := a.Vdb.GetSandbox(a.PFacts.SandboxName)
	if sb == nil {
		return scs, fmt.Errorf("could not find sandbox %s", a.PFacts.SandboxName)
	}
	scMap := a.Vdb.GenSubclusterMap()
	for i := range sb.Subclusters {
		sc := scMap[sb.Subclusters[i].Name]
		if sc == nil {
			return scs, fmt.Errorf("could not find subcluster %s", sb.Subclusters[i].Name)
		}
		targetType, found := sc.Annotations[vmeta.ParentSubclusterTypeAnnotation]
		if found && targetType == vapi.PrimarySubcluster && !sc.IsPrimary() {
			scs = append(scs, sc)
		}
	}
	return scs, nil
}

func (a *AlterSubclusterTypeReconciler) alterSubclusters(ctx context.Context, scs []*vapi.Subcluster) (ctrl.Result, error) {
	for _, sc := range scs {
		initiatorIP, requeue := a.getInitiatorIP()
		if requeue {
			return ctrl.Result{Requeue: requeue}, nil
		}
		err := a.alterSubclusterType(ctx, sc, initiatorIP)
		if err != nil {
			return ctrl.Result{}, err
		}
		a.PFacts.Invalidate()
		err = a.updateSubclusterTypeInVDB(ctx, sc)
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// updateSubclusterTypeInVDB updates the given subcluster's type in VDB
func (a *AlterSubclusterTypeReconciler) updateSubclusterTypeInVDB(ctx context.Context, sc *vapi.Subcluster) error {
	_, err := vk8s.UpdateVDBWithRetry(ctx, a.VRec, a.Vdb, func() (bool, error) {
		scMap := a.Vdb.GenSubclusterMap()
		vdbSc, found := scMap[sc.Name]
		if !found {
			return false, fmt.Errorf("subcluster %q missing in vdb %q", sc.Name, a.Vdb.Name)
		}
		vdbSc.Type = vapi.SandboxPrimarySubcluster
		return true, nil
	})
	return err
}

// alterSubclusterType changes the given subcluster's type
func (a *AlterSubclusterTypeReconciler) alterSubclusterType(ctx context.Context, sc *vapi.Subcluster,
	initiatorIP string) error {
	scType := vapi.SecondarySubcluster
	newType := vapi.PrimarySubcluster
	if sc.IsPrimary() {
		scType = vapi.PrimarySubcluster
		newType = vapi.SecondarySubcluster
	}
	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.AlterSubclusterStart,
		"Starting alter subcluster on %q from %q to %q", sc.Name, scType, newType)
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
	a.VRec.Eventf(a.Vdb, corev1.EventTypeNormal, events.AlterSubclusterSucceeded,
		"Successfully altered the type of subcluster %q to %q", sc.Name, newType)
	return nil
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
