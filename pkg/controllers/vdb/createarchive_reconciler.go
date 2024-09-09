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
	vmeta "github.com/vertica/vertica-kubernetes/pkg/meta"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
	corev1 "k8s.io/api/core/v1"
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
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveFailed,
		"DEBUG create archive !!!!!")
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveFailed,
		"DEBUG create archive !!!!!%+v", c.Vdb)
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveFailed,
		"DEBUG create archive !!!!!%s", vmeta.SaveRestorePointsTriggerID)
	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveFailed,
		"DEBUG create archive !!!!!%s", c.Vdb.Annotations[vmeta.SaveRestorePointsTriggerID])

	// Only proceed if the needed status condition is set.
	if c.Vdb.Annotations[vmeta.SaveRestorePointsTriggerID] == "true" {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveFailed,
			"DEBUG create archive !!!!! archive name: %s", c.Vdb.Spec.RestorePoint.Archive)
		err = c.runCreateArchiveVclusterAPI(ctx, "test", "", 0) // TODO: Fix with actual name
	}
	return ctrl.Result{}, err
}

// runVclusterAPI will do the actual execution of creating archive.
// This handles logging of necessary events.
func (c *CreateArchiveReconciler) runCreateArchiveVclusterAPI(ctx context.Context,
	archiveName string, sandbox string, numRestorePoint int) error {
	pf, ok := c.PFacts.findPodToRunAdminCmdAny()
	if !ok {
		// If no running pod found, then there is nothing to stop and we can just continue on
		return nil
	}
	opts := c.genOpts(pf.podIP, archiveName, numRestorePoint, sandbox)
	c.VRec.Event(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveStart, "Starting create archive")
	start := time.Now()

	if err := c.Dispatcher.CreateArchive(ctx, opts...); err != nil {
		c.VRec.Eventf(c.Vdb, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"Failed to create archive %q", archiveName)
		return err
	}

	c.VRec.Eventf(c.Vdb, corev1.EventTypeNormal, events.CreateArchiveSucceeded,
		"Successfully create archive. It took %s", time.Since(start).Truncate(time.Second))
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
