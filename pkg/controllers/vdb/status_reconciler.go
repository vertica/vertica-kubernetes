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
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// StatusReconciler will update the status field of the vdb.
type StatusReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Log          logr.Logger
	Vdb          *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PFacts       *PodFacts
	SkipShutdown bool
}

// MakeStatusReconciler will build a StatusReconciler object
func MakeStatusReconciler(cli client.Client, scheme *runtime.Scheme, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &StatusReconciler{
		Client:       cli,
		Scheme:       scheme,
		Log:          log.WithName("StatusReconciler"),
		Vdb:          vdb,
		PFacts:       pfacts,
		SkipShutdown: true,
	}
}

func MakeStatusReconcilerWithShutdown(cli client.Client, scheme *runtime.Scheme, log logr.Logger,
	vdb *vapi.VerticaDB, pfacts *PodFacts) controllers.ReconcileActor {
	return &StatusReconciler{
		Client:       cli,
		Scheme:       scheme,
		Log:          log.WithName("StatusReconciler"),
		Vdb:          vdb,
		PFacts:       pfacts,
		SkipShutdown: false,
	}
}

// Reconcile will update the status of the Vdb based on the pod facts
func (s *StatusReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// We base our status on the pod facts, so ensure our facts are up to date.
	if err := s.PFacts.Collect(ctx, s.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	if err := s.updateStatusFields(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if err := s.updateReadyStatusAnnotation(ctx); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// updateStatusFields will refresh the status fields in the vdb
func (s *StatusReconciler) updateStatusFields(ctx context.Context) error {
	finder := iter.MakeSubclusterFinder(s.Client, s.Vdb)

	refreshStatus := func(vdbChg *vapi.VerticaDB) error {
		// Use all subclusters regardless of the sandbox they belong to,
		// even ones that are scheduled for removal. We keep
		// reporting status on the deleted ones until the statefulsets are gone.
		subclusters, err := finder.FindSubclusters(ctx, iter.FindAllAcrossSandboxes, s.PFacts.GetSandboxName())
		if err != nil {
			return err
		}
		scSbMap := s.Vdb.GenSubclusterSandboxStatusMap()
		scMap := s.Vdb.GenSubclusterStatusMap()
		vdbChg.Status.Subclusters = []vapi.SubclusterStatus{}
		for i := range subclusters {
			if i == len(vdbChg.Status.Subclusters) {
				vdbChg.Status.Subclusters = append(vdbChg.Status.Subclusters, vapi.SubclusterStatus{
					Detail: make([]vapi.VerticaDBPodStatus, 0),
				})
			}
			sc := scMap[subclusters[i].Name]
			// A subcluster not being in status can only happen in
			// the main cluster. In that case, if the caller is the vdb
			// controller we will get the status from pod facts but if
			// the caller is sandbox controller then we can set an
			// an empty status knowing that the approriate controller
			// will take care of setting the status for this subcluster
			if sc != nil {
				// Preserve as much state as we can from the prior version. This
				// is necessary so we don't lose state in case all of the
				// subcluster pods are down
				vdbChg.Status.Subclusters[i] = *sc
			}
			// Sandboxed subclusters are always in vdb so,
			// all subclusters not in vdb will be considered
			// as part of the main cluster
			sbName := scSbMap[subclusters[i].Name]
			// The reconciler controls subclusters that are
			// part of the same cluster(main cluster or a sandbox)
			if sbName != s.PFacts.GetSandboxName() {
				continue
			}

			if !s.SkipShutdown {
				s.updateShutdownStatus(subclusters[i], &vdbChg.Status.Subclusters[i])
			}
			if err := s.calculateSubclusterStatus(ctx, subclusters[i], &vdbChg.Status.Subclusters[i]); err != nil {
				return fmt.Errorf("failed to calculate subcluster status %s %w", subclusters[i].Name, err)
			}
		}
		s.calculateClusterStatus(&vdbChg.Status)
		return nil
	}

	return vdbstatus.Update(ctx, s.Client, s.Vdb, refreshStatus)
}

// updateReadyStatusAnnotation will refresh the annotation we keep for ready status
func (s *StatusReconciler) updateReadyStatusAnnotation(ctx context.Context) error {
	nm := s.Vdb.ExtractNamespacedName()
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		if err := s.Client.Get(ctx, nm, s.Vdb); err != nil {
			if errors.IsNotFound(err) {
				return nil
			}
			return err
		}

		if s.Vdb.Annotations == nil {
			s.Vdb.Annotations = make(map[string]string, 1)
		}
		oldStatus := s.Vdb.Annotations[vmeta.ReadyStatusAnnotation]
		s.Vdb.Annotations[vmeta.ReadyStatusAnnotation] = fmt.Sprintf("%d/%d",
			s.Vdb.Status.UpNodeCount, s.Vdb.Status.AddedToDBCount)
		if oldStatus != s.Vdb.Annotations[vmeta.ReadyStatusAnnotation] {
			s.Log.Info("Refresh ready status annotation",
				"status", s.Vdb.Annotations[vmeta.ReadyStatusAnnotation])
			return s.Client.Update(ctx, s.Vdb)
		}
		return nil
	})
}

// calculateClusterStatus will roll up the subcluster status.
func (s *StatusReconciler) calculateClusterStatus(stat *vapi.VerticaDBStatus) {
	stat.SubclusterCount = 0
	stat.AddedToDBCount = 0
	stat.UpNodeCount = 0
	for _, sc := range stat.Subclusters {
		stat.SubclusterCount++
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
		stsSize := sc.GetStsSize(s.Vdb)
		if stsSize == 0 && stsSize != sc.Size {
			s.setSubclusterStatusWhenShutdown(podIndex, curStat)
			// At this point the subcluster pods have been deleted
			// but we do not want to lose info like vnodename or subclusteroid
			// so we jump to the next subcluster.
			continue
		}
		curStat.Detail[podIndex].UpNode = pf.upNode
		curStat.Detail[podIndex].Installed = pf.isInstalled
		curStat.Detail[podIndex].AddedToDB = pf.dbExists
		if pf.vnodeName != "" {
			curStat.Detail[podIndex].VNodeName = pf.vnodeName
		}
		if pf.subclusterOid != "" {
			curStat.Oid = pf.subclusterOid
		}
	}
	// Refresh the counts
	curStat.AddedToDBCount = 0
	curStat.UpNodeCount = 0
	for _, v := range curStat.Detail {
		if v.AddedToDB {
			curStat.AddedToDBCount++
		}
		if v.UpNode {
			curStat.UpNodeCount++
		}
	}
	return nil
}

// setSubclusterStatusWhenShutdown sets some subcluster status fields
// when it is shutdown.
func (s *StatusReconciler) setSubclusterStatusWhenShutdown(podIndex int32, curStat *vapi.SubclusterStatus) {
	curStat.Detail[podIndex].UpNode = false
	curStat.Detail[podIndex].Installed = false
	curStat.Detail[podIndex].AddedToDB = false
}

func (s *StatusReconciler) updateShutdownStatus(sc *vapi.Subcluster, curStat *vapi.SubclusterStatus) {
	curStat.Shutdown = sc.Shutdown
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
