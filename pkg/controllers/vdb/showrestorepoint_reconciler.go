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
	"time"

	"github.com/go-logr/logr"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ShowRestorePointReconciler will query a restore point and update the status
type ShowRestorePointReconciler struct {
	VRec       *VerticaDBReconciler
	Vdb        *vapi.VerticaDB
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *PodFacts
	config.ConfigParamsGenerator
}

func MakeShowRestorePointReconciler(r *VerticaDBReconciler, vdb *vapi.VerticaDB, log logr.Logger,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &ShowRestorePointReconciler{
		VRec:       r,
		Log:        log.WithName("ShowRestorePointReconciler"),
		Vdb:        vdb,
		Dispatcher: dispatcher,
		PFacts:     pfacts,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: r,
			Vdb:  vdb,
			Log:  log.WithName("ShowRestorePointReconciler"),
		},
	}
}

func (s *ShowRestorePointReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	if shouldEarlyExit(s.Vdb) {
		return ctrl.Result{}, nil
	}

	restorePoints, requeue, err := s.runShowRestorePoints(ctx)
	if err != nil || requeue {
		return ctrl.Result{Requeue: requeue}, err
	}
	return ctrl.Result{}, s.saveRestorePointDetailsInVDB(ctx, restorePoints)
}

// runShowRestorePoints call the vclusterOps API to get the restore points
func (s *ShowRestorePointReconciler) runShowRestorePoints(ctx context.Context) ([]vclusterops.RestorePoint, bool, error) {
	finder := iter.MakeSubclusterFinder(s.VRec.GetClient(), s.Vdb)
	pods, err := finder.FindPods(ctx, iter.FindExisting, vapi.MainCluster)
	if err != nil {
		return nil, false, err
	}

	// find a pod to execute the vclusterops API
	podIP := vk8s.FindRunningPodWithNMAContainer(pods)
	if podIP == "" {
		s.Log.Info("couldn't find any pod to run the query")
		return nil, true, nil
	}

	// extract out the communal and config information to pass down to the vclusterops API.
	opts := []showrestorepoints.Option{}
	opts = append(opts,
		showrestorepoints.WithInitiator(podIP),
		showrestorepoints.WithCommunalPath(s.Vdb.GetCommunalPath()),
		showrestorepoints.WithConfigurationParams(s.ConfigurationParams.GetMap()),
		showrestorepoints.WithArchiveNameFilter(s.Vdb.Status.RestorePoint.Archive),
		showrestorepoints.WithStartTimestampFilter(s.Vdb.Status.RestorePoint.StartTimestamp),
		showrestorepoints.WithEndTimestampFilter(s.Vdb.Status.RestorePoint.EndTimestamp),
	)

	// call showRestorePoints vcluster API
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.ShowRestorePointsStarted,
		"Starting show restore points")
	start := time.Now()
	restorePoints, err := s.Dispatcher.ShowRestorePoints(ctx, opts...)
	if err != nil {
		return nil, false, err
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.ShowRestorePointsSucceeded,
		"Successfully queried restore points in %s", time.Since(start).Truncate(time.Second))
	return restorePoints, false, nil
}

// saveRestorePointDetailsInVDB saves the last created restore point info in the status.
func (s *ShowRestorePointReconciler) saveRestorePointDetailsInVDB(ctx context.Context, restorePoints []vclusterops.RestorePoint) error {
	// This is very unlikely to happen
	if len(restorePoints) == 0 {
		s.Log.Info("No restore point found.")
		return nil
	}
	// We should normally only be getting the restore point that was just created
	// so if there are more that one restore point we will randomly pick the first one
	if len(restorePoints) > 1 {
		s.Log.Info("Multiple restore points were found, only the first one will be saved in the status.")
	}
	restorePoint := restorePoints[0]
	return s.updateRestorePointDetails(ctx, &restorePoint)
}

func (s *ShowRestorePointReconciler) updateRestorePointDetails(ctx context.Context, restorePoint *vclusterops.RestorePoint) error {
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		if vdb.Status.RestorePoint == nil {
			vdb.Status.RestorePoint = new(vapi.RestorePointInfo)
		}
		if vdb.Status.RestorePoint.Details == nil {
			vdb.Status.RestorePoint.Details = new(vclusterops.RestorePoint)
		}
		vdb.Status.RestorePoint.Details.Archive = restorePoint.Archive
		vdb.Status.RestorePoint.Details.Index = restorePoint.Index
		vdb.Status.RestorePoint.Details.ID = restorePoint.ID
		vdb.Status.RestorePoint.Details.Timestamp = restorePoint.Timestamp
		vdb.Status.RestorePoint.Details.VerticaVersion = restorePoint.VerticaVersion
		return nil
	}
	return vdbstatus.Update(ctx, s.VRec.Client, s.Vdb, refreshStatusInPlace)
}

func shouldEarlyExit(vdb *vapi.VerticaDB) bool {
	return vdb.Status.RestorePoint == nil ||
		vdb.Status.RestorePoint.Archive == "" ||
		vdb.Status.RestorePoint.StartTimestamp == "" ||
		vdb.Status.RestorePoint.EndTimestamp == "" ||
		vdb.Status.RestorePoint.Details != nil
}
