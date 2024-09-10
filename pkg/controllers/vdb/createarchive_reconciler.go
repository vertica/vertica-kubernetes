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
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
	"github.com/vertica/vertica-kubernetes/pkg/vdbstatus"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CreateArchiveReconciler will unsandbox subclusters in the sandbox of a sandbox config map
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

// Reconcile will stop vertica if the status condition indicates a restart is needed
func (c *CreateArchiveReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	err := c.PFacts.Collect(ctx, c.Vdb)
	if err != nil {
		return ctrl.Result{}, err
	}

	// No-op if no database exists
	if !c.PFacts.doesDBExist() {
		return ctrl.Result{}, nil
	}

	// Only proceed if the SaveRestorePointsNeeded status condition is set to true.
	if c.Vdb.IsStatusConditionTrue(vapi.SaveRestorePointsNeeded) {
		if c.Vdb.Spec.RestorePoint != nil && c.Vdb.Spec.RestorePoint.Archive != "" {
			// params: context, archive-name, sandbox, num of restore point(0 is unlimited)
			err = c.runCreateArchiveVclusterAPI(ctx, c.Vdb.Spec.RestorePoint.Archive, "", 0)
			if err != nil {
				return ctrl.Result{}, err
			}
		}
		// param not set correctly, Log warning
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"create archive failed, archive name not set in RestorePoint.")
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}

// runVclusterAPI will do the actual execution of creating archive.
// This handles logging of necessary events.
func (c *CreateArchiveReconciler) runCreateArchiveVclusterAPI(ctx context.Context,
	archiveName string, sandbox string, numRestorePoint int) error {
	hostIP, ok := c.PFacts.FindFirstUpPodIP(true, "")
	if !ok {
		// If no running pod found, then there is nothing to stop and we can just continue on
		return nil
	}
	opts := c.genOpts(hostIP, archiveName, numRestorePoint, sandbox)
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveStart, "Starting create archive")
	start := time.Now()

	if err := c.Dispatcher.CreateArchive(ctx, opts...); err != nil {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"Failed to create archive %q", archiveName)
		return err
	}

	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveSucceeded,
		"Successfully create archive. It took %s", time.Since(start).Truncate(time.Second))

	// Clear the condition after archive creation success.
	err := vdbstatus.UpdateCondition(ctx, c.VRec.Client, c.Vdb,
		vapi.MakeCondition(vapi.SaveRestorePointsNeeded,
			metav1.ConditionFalse, "ArchiveCreationCompleted"),
	)
	if err != nil {
		return err
	}
	return nil
}

// genOpts will return the options to use with the create archive command
func (c *CreateArchiveReconciler) genOpts(initiatorIP string, archiveName string,
	numRestorePoint int, sandbox string) []createarchive.Option {
	opts := []createarchive.Option{
		createarchive.WithInitiator(initiatorIP),
		createarchive.WithArchiveName(archiveName),
		createarchive.WithNumOfArchives(numRestorePoint),
		createarchive.WithSandbox(sandbox),
	}
	return opts
}
