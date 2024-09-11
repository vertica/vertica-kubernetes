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
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/saverestorepoint"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateArchiveReconciler will create archive and save restore point if triggered
type CreateArchiveReconciler struct {
	VRec *VerticaDBReconciler
	Vdb  *vapi.VerticaDB
	Log  logr.Logger
	client.Client
	Dispatcher     vadmin.Dispatcher
	PFacts         *PodFacts
	OriginalPFacts *PodFacts
	InitiatorIP    string // The IP of the pod that we run vclusterOps from
}

func MakeCreateArchiveReconciler(r *VerticaDBReconciler, vdb *vapi.VerticaDB, log logr.Logger,
	pfacts *PodFacts, dispatcher vadmin.Dispatcher, cli client.Client) controllers.ReconcileActor {
	pfactsForMainCluster := pfacts.Copy(vapi.MainCluster)
	return &CreateArchiveReconciler{
		VRec:           r,
		Log:            log.WithName("CreateArchiveReconciler"),
		Vdb:            vdb,
		Client:         cli,
		Dispatcher:     dispatcher,
		PFacts:         &pfactsForMainCluster,
		OriginalPFacts: pfacts,
	}
}

// Reconcile will create an archive if the status condition indicates true
// And will save restore point to the created arcihve if restorePoint.archive value
// is provided in the CRD spec
func (c *CreateArchiveReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := c.PFacts.Collect(ctx, c.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if no database exists
	if !c.PFacts.doesDBExist() {
		return ctrl.Result{}, nil
	}

	hostIP, ok := c.PFacts.FindFirstUpPodIP(true, "")
	if !ok {
		// If no running pod found, then there is nothing to do and we can just continue on
		return ctrl.Result{}, nil
	}

	// Only proceed if the SaveRestorePointsNeeded status condition is set to true.
	if c.Vdb.IsStatusConditionTrue(vapi.SaveRestorePointsNeeded) {
		if c.Vdb.Spec.RestorePoint != nil && c.Vdb.Spec.RestorePoint.Archive != "" {
			// Always tried to create archive
			// params: context, host, archive-name, sandbox, num of restore point(0 is unlimited)
			err = c.runCreateArchiveVclusterAPI(ctx, hostIP, c.Vdb.Spec.RestorePoint.Archive, "", 0)
			if err != nil {
				return ctrl.Result{}, err
			}
			// Once save restore point, change condition
			// params: context, host, archive-name, sandbox, num of restore point(0 is unlimited)
			err = c.runSaveRestorePointVclusterAPI(ctx, hostIP, c.Vdb.Spec.RestorePoint.Archive, "")
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		// archinve name param not set correctly, Log warning
		c.Log.Info("create archive failed, archive name not set in restorePoint spec.")
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// runCreateArchiveVclusterAPI will do the actual execution of creating archive.
// This handles logging of necessary events.
func (c *CreateArchiveReconciler) runCreateArchiveVclusterAPI(ctx context.Context,
	host string, archiveName string, sandbox string, numRestorePoint int) error {

	opts := c.genCreateArchiveOpts(host, archiveName, numRestorePoint, sandbox)
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveStart, "Starting create archive")
	start := time.Now()

	// Always try to create
	err := c.Dispatcher.CreateArchive(ctx, opts...)
	if err != nil {
		// If already exist, ignore error, log warning
		if strings.Contains(err.Error(), "Duplicate object on host") {
			c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.ArchiveExists,
				"archive: %s already exist", archiveName)
			return nil
		}
		// For all other errors, return error
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"Failed to create archive %q", archiveName)
		return err
	}

	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveSucceeded,
		"Successfully create archive. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}

// runSaveRestorePointVclusterAPI will do the actual execution of saving restore point to
// an existing archive.
// This handles logging of necessary events.
func (c *CreateArchiveReconciler) runSaveRestorePointVclusterAPI(ctx context.Context,
	host string, archiveName string, sandbox string) error {
	opts := c.genSaveRestorePointOpts(host, archiveName, sandbox)
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal,
		events.SaveRestorePointStart, "Starting save restore point")
	start := time.Now()

	err := c.Dispatcher.SaveRestorePoint(ctx, opts...)
	if err != nil {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.SaveRestorePointFailed,
			"Failed to save restore point to archive: %s", archiveName)
		return err
	}
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.SaveRestorePointSucceeded,
		"Successfully save restore point to archive: %s. It took %s",
		archiveName, time.Since(start).Truncate(time.Second))

	// Clear the condition after archive creation success.
	err = vdbstatus.UpdateCondition(ctx, c.VRec.Client, c.Vdb,
		vapi.MakeCondition(vapi.SaveRestorePointsNeeded,
			metav1.ConditionFalse, "Completed"),
	)
	if err != nil {
		return err
	}
	return nil
}

// genCreateArchiveOpts will return the options to use with the create archive command
func (c *CreateArchiveReconciler) genCreateArchiveOpts(initiatorIP string, archiveName string,
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
func (c *CreateArchiveReconciler) genSaveRestorePointOpts(initiatorIP string, archiveName string,
	sandbox string) []saverestorepoint.Option {
	opts := []saverestorepoint.Option{
		saverestorepoint.WithInitiator(initiatorIP),
		saverestorepoint.WithArchiveName(archiveName),
		saverestorepoint.WithSandbox(sandbox),
	}
	return opts
}
