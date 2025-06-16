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
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/altersc"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
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
	IsSandbox  bool // IsSandbox is true if this is a sandbox subcluster
}

// MakeAlterSubclusterTypeReconciler will build a AlterSubclusterTypeReconciler object
func MakeAlterSubclusterTypeReconciler(vdbrecon config.ReconcilerInterface, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher, isSandbox bool) controllers.ReconcileActor {
	return &AlterSubclusterTypeReconciler{
		VRec:       vdbrecon,
		Log:        log.WithName("AlterSubclusterTypeReconciler"),
		Vdb:        vdb,
		PFacts:     pfacts,
		Dispatcher: dispatcher,
		IsSandbox:  isSandbox,
	}
}

func (a *AlterSubclusterTypeReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if a.PFacts.SandboxName == vapi.MainCluster {
		return ctrl.Result{}, nil
	}

	if err := a.PFacts.Collect(ctx, a.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	var scs []*vapi.Subcluster
	var err error
	if a.IsSandbox {
		scs, err = a.findSandboxSubclustersToAlter()
	} else {
		scs, err = a.findMainSubclustersToAlter()
	}
	if err != nil || len(scs) == 0 {
		return ctrl.Result{}, err
	}

	return a.alterSubclusters(ctx, scs)
}

// findMainSubclustersToAlter returns a list of main/sandbox subclusters whose type needs to be changed
//
//nolint:unparam
func (a *AlterSubclusterTypeReconciler) findMainSubclustersToAlter() ([]*vapi.Subcluster, error) {
	scs := []*vapi.Subcluster{}
	scMap := a.Vdb.GenSubclusterMap()

	for scName, sc := range scMap {
		// find the subcluster whose type is different to podfacts (which reads the database)
		pf, ok := a.PFacts.FindFirstUpPod(false, scName)
		if !ok {
			continue
		}
		if sc.Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sc.Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
			scs = append(scs, sc)
		}
	}
	return scs, nil
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

		// for sandbox subcluster with multiple primary subclusters, vclusterops only
		// set the first one as primary
		// if sandbox subcluster type is primary but podfacts (from database) is_primary is false,
		// we need to change the subcluster is_primary to true in the database
		pf, ok := a.PFacts.FindFirstUpPod(false, sc.Name)
		// skip if one of the pods in the subcluster isn't found
		if !ok {
			continue
		}
		if sb.Subclusters[i].Type == vapi.PrimarySubcluster && !pf.GetIsPrimary() ||
			sb.Subclusters[i].Type == vapi.SecondarySubcluster && pf.GetIsPrimary() {
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
	}
	return ctrl.Result{}, nil
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

	a.PFacts.Invalidate()
	pf.SetIsPrimary(newType == vapi.PrimarySubcluster)
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
