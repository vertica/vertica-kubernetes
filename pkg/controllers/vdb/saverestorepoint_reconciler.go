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
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/saverestorepoint"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SaveRestorePoint will create archive and save restore point if triggered
type SaveRestorePoint struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB
	Log  logr.Logger
	client.Client
	Dispatcher     vadmin.Dispatcher
	PFacts         *PodFacts
	OriginalPFacts *PodFacts
	InitiatorIP    string // The IP of the pod that we run vclusterOps from
}

func MakeSaveRestorePointReconciler(r *VerticaDBReconciler, vdb *vapi.VerticaDB, log logr.Logger,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client) controllers.ReconcileActor {
	pfactsForMainCluster := pfacts.Copy(vapi.MainCluster)
	return &SaveRestorePoint{
		VRec:           r,
		Log:            log.WithName("SaveRestorePoint"),
		Vdb:            vdb,
		Client:         cli,
		Dispatcher:     dispatcher,
		PFacts:         &pfactsForMainCluster,
		OriginalPFacts: pfacts,
	}
}

// Reconcile will create an archive if the status condition indicates true
// And will save restore point to the created archive if restorePoint.archive value
// is provided in the CRD spec
func (s *SaveRestorePoint) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Only proceed if the SaveRestorePointsNeeded status condition is set to true.
	if !s.Vdb.IsStatusConditionTrue(vapi.SaveRestorePointsNeeded) {
		return ctrl.Result{}, nil
	}
	// Check if deployment is using vclusterops
	if !vmeta.UseVClusterOps(s.Vdb.Annotations) {
		s.VRec.Event(s.Vdb, corev1.EventTypeWarning, events.InDBSaveRestorePointNotSupported,
			"SaveRestorePoint is not supported for admintools deployments")
		err := vdbstatus.UpdateCondition(ctx, s.VRec.Client, s.Vdb,
			vapi.MakeCondition(vapi.SaveRestorePointsNeeded,
				metav1.ConditionFalse, "AdmintoolsNotSupported"),
		)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	// Ensure vertica version supports in-database SaveRestorePoint vcluster API
	vinf, err := s.Vdb.MakeVersionInfoCheck()
	if err != nil {
		return ctrl.Result{}, err
	}
	if !vinf.IsEqualOrNewer(vapi.SaveRestorePointNMAOpsMinVersion) {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.UnsupportedVerticaVersion,
			"The Vertica version %q doesn't support create restore points. The minimum version supported is %s.",
			vinf.VdbVer, vapi.SaveRestorePointNMAOpsMinVersion)
		err = vdbstatus.UpdateCondition(ctx, s.VRec.Client, s.Vdb,
			vapi.MakeCondition(vapi.SaveRestorePointsNeeded,
				metav1.ConditionFalse, "IncompatibleDB"),
		)
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{}, nil
	}

	err = s.PFacts.Collect(ctx, s.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	hostIP, ok := s.PFacts.FindFirstUpPodIP(false, "")
	if !ok {
		s.Log.Info("No running pod for create restore point. Requeuing.")
		return ctrl.Result{Requeue: true}, nil
	}

	// Check if required attribute is set in spec: restorePoint.archive
	if s.Vdb.Spec.RestorePoint != nil && s.Vdb.Spec.RestorePoint.Archive != "" {
		// Always tried to create archive
		// params: context, host, archive-name, sandbox, num of restore point(0 is unlimited)
		err = s.runCreateArchiveVclusterAPI(ctx, hostIP, s.Vdb.Spec.RestorePoint.Archive, "", 0)
		if err != nil {
			return ctrl.Result{}, err
		}
		// Once save restore point, change condition
		// params: context, host, archive-name, sandbox, num of restore point(0 is unlimited)
		err = s.runSaveRestorePointVclusterAPI(ctx, hostIP, s.Vdb.Spec.RestorePoint.Archive, "")
		if err != nil {
			return ctrl.Result{}, err
		}
	}
	// archive name param not set correctly, Log warning
	s.Log.Info("create archive failed, archive name not set in restorePoint spec.")
	return ctrl.Result{Requeue: true}, nil
}

// runCreateArchiveVclusterAPI will do the actual execution of creating archive.
// This handles logging of necessary events.
func (s *SaveRestorePoint) runCreateArchiveVclusterAPI(ctx context.Context,
	host string, archiveName string, sandbox string, numRestorePoint int) error {
	opts := s.genCreateArchiveOpts(host, archiveName, numRestorePoint, sandbox)
	s.VRec.Event(s.Vdb, corev1.EventTypeNormal, events.CreateArchiveStart, "Starting create archive")
	start := time.Now()

	// Always try to create
	err := s.Dispatcher.CreateArchive(ctx, opts...)
	if err != nil {
		// If already exist, ignore error, log warning
		if strings.Contains(err.Error(), "Duplicate object on host") {
			s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.ArchiveExists,
				"archive: %s already exist", archiveName)
			return nil
		}
		// For all other errors, return error
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"Failed to create archive %q", archiveName)
		return err
	}

	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.CreateArchiveSucceeded,
		"Successfully create archive. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}

// runSaveRestorePointVclusterAPI will do the actual execution of saving restore point to
// an existing archive.
// This handles logging of necessary events.
func (s *SaveRestorePoint) runSaveRestorePointVclusterAPI(ctx context.Context,
	host string, archiveName string, sandbox string) error {
	opts := s.genSaveRestorePointOpts(host, archiveName, sandbox)
	s.VRec.Event(s.Vdb, corev1.EventTypeNormal,
		events.SaveRestorePointStart, "Starting save restore point")
	start := time.Now()

	err := s.Dispatcher.SaveRestorePoint(ctx, opts...)
	if err != nil {
		s.VRec.Eventf(s.Vdb, corev1.EventTypeWarning, events.SaveRestorePointFailed,
			"Failed to save restore point to archive: %s", archiveName)
		return err
	}
	end := time.Now()
	s.VRec.Eventf(s.Vdb, corev1.EventTypeNormal, events.SaveRestorePointSucceeded,
		"Successfully save restore point to archive: %s. It took %s",
		archiveName, time.Since(start).Truncate(time.Second))
	refreshStatusInPlace := func(vdb *vapi.VerticaDB) error {
		meta.SetStatusCondition(&vdb.Status.Conditions, *vapi.MakeCondition(vapi.SaveRestorePointsNeeded,
			metav1.ConditionFalse, "Completed"))
		if vdb.Status.RestorePoint == nil {
			vdb.Status.RestorePoint = new(vapi.RestorePointInfo)
		}
		vdb.Status.RestorePoint.Archive = archiveName
		vdb.Status.RestorePoint.StartTimestamp = start.Format("2006-01-02 15:04:05.000000000")
		vdb.Status.RestorePoint.EndTimestamp = end.Format("2006-01-02 15:04:05.000000000")
		return nil
	}
	// Clear the condition and add a status after restore point creation.
	err = vdbstatus.Update(ctx, s.VRec.Client, s.Vdb, refreshStatusInPlace)
	if err != nil {
		return err
	}
	return nil
}

// genCreateArchiveOpts will return the options to use with the create archive command
func (s *SaveRestorePoint) genCreateArchiveOpts(initiatorIP string, archiveName string,
	numRestorePoint int, sandbox string) []createarchive.Option {
	opts := []createarchive.Option{
		createarchive.WithInitiator(initiatorIP),
		createarchive.WithArchiveName(archiveName),
		createarchive.WithNumRestorePoints(numRestorePoint),
		createarchive.WithSandbox(sandbox),
	}
	return opts
}

// genSaveRestorePointOpts will return the options to use with the create archive command
func (s *SaveRestorePoint) genSaveRestorePointOpts(initiatorIP string, archiveName string,
	sandbox string) []saverestorepoint.Option {
	opts := []saverestorepoint.Option{
		saverestorepoint.WithInitiator(initiatorIP),
		saverestorepoint.WithArchiveName(archiveName),
		saverestorepoint.WithSandbox(sandbox),
	}
	return opts
}
