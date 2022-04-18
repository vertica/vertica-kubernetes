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

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusReconciler will update the status field of the vdb.
type StatusReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
	Vdb    *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts *PodFacts
}

// MakeStatusReconciler will build a StatusReconciler object
func MakeStatusReconciler(cli client.Client, scheme *runtime.Scheme, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &StatusReconciler{Client: cli, Scheme: scheme, Log: log, Vdb: vdb, PFacts: pfacts}
}

// Reconcile will update the status of the Vdb based on the pod facts
func (s *StatusReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// We base our status on the pod facts, so ensure our facts are up to date.
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Use all subclusters, even ones that are scheduled for removal.  We keep
	// reporting status on the deleted ones until the statefulsets are gone.
	finder := iter.MakeSubclusterFinder(s.Client, s.Vdb)
	subclusters, err := finder.FindSubclusters(ctx, iter.FindAll)
	if err != nil {
		return ctrl.Result{}, err
	}

	refreshStatus := func(vdbChg *vapi.VerticaDB) error {
		vdbChg.Status.Subclusters = []vapi.SubclusterStatus{}
		for i := range subclusters {
			if i == len(vdbChg.Status.Subclusters) {
				vdbChg.Status.Subclusters = append(vdbChg.Status.Subclusters, vapi.SubclusterStatus{})
			}
			if err := s.calculateSubclusterStatus(ctx, subclusters[i], &vdbChg.Status.Subclusters[i]); err != nil {
				return fmt.Errorf("failed to calculate subcluster status %s %w", subclusters[i].Name, err)
			}
		}
		s.calculateClusterStatus(&vdbChg.Status)
		return nil
	}

	if err := vdbstatus.Update(ctx, s.Client, s.Vdb, refreshStatus); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// calculateClusterStatus will roll up the subcluster status.
func (s *StatusReconciler) calculateClusterStatus(stat *vapi.VerticaDBStatus) {
	stat.SubclusterCount = 0
	stat.InstallCount = 0
	stat.AddedToDBCount = 0
	stat.UpNodeCount = 0
	for _, sc := range stat.Subclusters {
		stat.SubclusterCount++
		stat.InstallCount += sc.InstallCount
		stat.AddedToDBCount += sc.AddedToDBCount
		stat.UpNodeCount += sc.UpNodeCount
	}
}

// calculateSubclusterStatus will figure out the status for the given subcluster
func (s *StatusReconciler) calculateSubclusterStatus(ctx context.Context, sc *vapi.Subcluster, curStat *vapi.SubclusterStatus) error {
	curStat.Name = sc.Name

	if err := s.resizeSubclusterStatus(ctx, sc, curStat); err != nil {
		return err
	}

	for podIndex := int32(0); podIndex < int32(len(curStat.Detail)); podIndex++ {
		podName := names.GenPodName(s.Vdb, sc, podIndex)
		pf, ok := s.PFacts.Detail[podName]
		if !ok {
			continue
		}
		curStat.Detail[podIndex].UpNode = pf.upNode
		curStat.Detail[podIndex].ReadOnly = pf.readOnly
		// We can only reliably update the status for running pods. Skip those
		// that we couldn't figure out to preserve their state.
		if !pf.isInstalled.IsNone() {
			curStat.Detail[podIndex].Installed = pf.isInstalled.IsTrue()
		}
		// Similar comment about db existence and vertica node name. Skip pods
		// that we couldn't figure out the state for.
		if !pf.dbExists.IsNone() {
			curStat.Detail[podIndex].AddedToDB = pf.dbExists.IsTrue()
			curStat.Detail[podIndex].VNodeName = pf.vnodeName
		}
	}
	// Refresh the counts
	curStat.InstallCount = 0
	curStat.AddedToDBCount = 0
	curStat.UpNodeCount = 0
	curStat.ReadOnlyCount = 0
	for _, v := range curStat.Detail {
		if v.Installed {
			curStat.InstallCount++
		}
		if v.AddedToDB {
			curStat.AddedToDBCount++
		}
		if v.UpNode {
			curStat.UpNodeCount++
		}
		if v.ReadOnly {
			curStat.ReadOnlyCount++
		}
	}
	return nil
}

// resizeSubclusterStatus will set the size of curStat.Detail to its correct value.
// The size of the detail must match the current size of the subcluster.  The detail
// could grow or shrink.
func (s *StatusReconciler) resizeSubclusterStatus(ctx context.Context, sc *vapi.Subcluster, curStat *vapi.SubclusterStatus) error {
	scSize, err := s.getSubclusterSize(ctx, sc)
	if err != nil {
		return err
	}
	// Grow the detail if needed
	for ok := true; ok; ok = int32(len(curStat.Detail)) < scSize {
		curStat.Detail = append(curStat.Detail, vapi.VerticaDBPodStatus{})
	}
	// Or shrink the size
	if int32(len(curStat.Detail)) > scSize {
		curStat.Detail = curStat.Detail[0:scSize]
	}
	return nil
}

// getSubclusterSize returns the size of the given subcluster.
// If there is a decrepency between sts and vdb, we pick the max.
func (s *StatusReconciler) getSubclusterSize(ctx context.Context, sc *vapi.Subcluster) (int32, error) {
	// We return the max of the replica count in the sts and subcluster size.  The
	// two can differ due to a partial reconciliation.
	maxSize := sc.Size

	sts := &appsv1.StatefulSet{}
	if err := s.Client.Get(ctx, names.GenStsName(s.Vdb, sc), sts); err != nil {
		// If the sts isn't found, we default to the size of the subcluster from vapi.Subcluster
		if errors.IsNotFound(err) {
			return sc.Size, nil
		}
		return 0, fmt.Errorf("could not fetch sts for subcluster %w", err)
	}

	if sts.Status.Replicas > maxSize {
		maxSize = sts.Status.Replicas
	}
	return maxSize, nil
}
