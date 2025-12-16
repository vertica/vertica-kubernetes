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

package vrpq

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1 "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	verrors "github.com/vertica/vertica-kubernetes/pkg/errors"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/iter"
	"github.com/vertica/vertica-kubernetes/pkg/podfacts"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/createarchive"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/saverestorepoint"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/showrestorepoints"
	config "github.com/vertica/vertica-kubernetes/pkg/vdbconfig"
	"github.com/vertica/vertica-kubernetes/pkg/vk8s"
	vrpqstatus "github.com/vertica/vertica-kubernetes/pkg/vrpqstatus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	// Status states for the query lifecycle
	stateQuerying     = "Querying"
	stateSuccessQuery = "Query successful"
	stateFailedQuery  = "Query failed"
	// Pod phase constant
	podRunning = "Running"
)

// QueryReconciler handles the reconciliation logic for VerticaRestorePointsQuery operations.
// It orchestrates archive creation, restore point management, and restore operations.
type QueryReconciler struct {
	VRec       *VerticaRestorePointsQueryReconciler
	Vrpq       *v1beta1.VerticaRestorePointsQuery
	Vdb        *vapi.VerticaDB
	Log        logr.Logger
	Dispatcher vadmin.Dispatcher
	PFacts     *podfacts.PodFacts
	config.ConfigParamsGenerator
}

// MakeRestorePointsQueryReconciler creates a new QueryReconciler instance with all required dependencies.
// This factory function initializes the reconciler with the necessary context for executing
// restore point queries, archive operations, and restore operations against a VerticaDB.
func MakeRestorePointsQueryReconciler(r *VerticaRestorePointsQueryReconciler, vrpq *v1beta1.VerticaRestorePointsQuery,
	vdb *vapi.VerticaDB, log logr.Logger, pfacts *podfacts.PodFacts, dispatcher vadmin.Dispatcher) controllers.ReconcileActor {
	return &QueryReconciler{
		VRec:       r,
		Vrpq:       vrpq,
		Log:        log.WithName("QueryReconciler"),
		Vdb:        vdb,
		Dispatcher: dispatcher,
		PFacts:     pfacts,
		ConfigParamsGenerator: config.ConfigParamsGenerator{
			VRec: r,
			Log:  log.WithName("QueryReconciler"),
			Vdb:  vdb,
		},
	}
}

// Reconcile performs the main reconciliation logic for restore point queries.
// It validates the query state, collects necessary information from the VerticaDB,
// identifies an appropriate pod to run the operation, and dispatches the query
// based on the query type (ShowRestorePoints, SaveRestorePoint, etc.).
func (q *QueryReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	// Skip reconciliation if query has already completed (successfully or with failure)
	isPresent := q.Vrpq.IsStatusConditionPresent(v1beta1.QueryComplete)
	if isPresent {
		return ctrl.Result{}, nil
	}

	// Skip reconciliation if the query is not ready to be executed
	isSet := q.Vrpq.IsStatusConditionFalse(v1beta1.QueryReady)
	if isSet {
		return ctrl.Result{}, nil
	}

	// Collect communal storage configuration and other details from the VerticaDB
	if res, err := q.collectInfoFromVdb(ctx); verrors.IsReconcileAborted(res, err) {
		return res, err
	}

	// Gather current state of all pods in the main cluster
	if err := q.PFacts.Collect(ctx, q.Vdb); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure there's at least one UP node available to execute the query
	if q.PFacts.GetUpNodeAndNotReadOnlyCount() == 0 {
		q.Log.Info("No up nodes available to run the query. Requeuing")
		return ctrl.Result{Requeue: true}, nil
	}

	// Select a running pod with NMA container ready to act as the initiator
	podIP, res, errInit := q.getInitiatorPodIP(ctx)
	q.Log.Info("Selected initiator pod IP", "podIP", podIP, "res", res, "errInit", errInit)
	if verrors.IsReconcileAborted(res, errInit) {
		return res, errInit
	}

	// Dispatch to the appropriate handler based on query type
	var errQuery error
	switch {
	case q.Vrpq.IsShowRestorePointsQuery():
		errQuery = q.showRestorePoints(ctx, podIP)
	case q.Vrpq.IsSaveRestorePointQuery():
		errQuery = q.saveRestorePoint(ctx, podIP)
	}

	return ctrl.Result{}, errQuery
}

// showRestorePoints executes a query to list all restore points based on the provided filter criteria.
// It constructs the necessary options including communal path, configuration parameters,
// and optional filters (archive name, time range) before calling the vclusterops API.
func (q *QueryReconciler) showRestorePoints(ctx context.Context, podIP string) error {
	// Build options for the show restore points API call
	opts := []showrestorepoints.Option{}
	opts = append(opts,
		showrestorepoints.WithInitiator(q.Vrpq.ExtractNamespacedName(), podIP),
		showrestorepoints.WithCommunalPath(q.Vdb.GetCommunalPath()),
		showrestorepoints.WithConfigurationParams(q.ConfigurationParams.GetMap()),
	)

	// Apply optional filter criteria if specified
	if filter := q.Vrpq.Spec.FilterOptions; filter != nil {
		opts = append(opts,
			showrestorepoints.WithArchiveNameFilter(filter.ArchiveName),
			showrestorepoints.WithStartTimestampFilter(filter.StartTimestamp),
			showrestorepoints.WithEndTimestampFilter(filter.EndTimestamp),
		)
	}
	return q.runShowRestorePoints(ctx, opts)
}

// saveRestorePoint creates an archive (if it doesn't exist) and saves a new restore point to it.
// This operation is a two-step process:
// 1. Ensure the archive exists by attempting to create it (idempotent operation)
// 2. Save a new restore point to the archive
func (q *QueryReconciler) saveRestorePoint(ctx context.Context, podIP string) error {
	// Validate that an archive name is specified
	if q.Vrpq.Spec.SaveOptions.Archive == "" {
		return errors.New("cannot save restore point when spec.SaveOptions.Archive is not set")
	}

	// Step 1: Ensure the archive exists (create if needed)
	// Parameters: context, initiator host IP, archive name, sandbox name, number of restore points limit
	err := q.createArchiveIfNeeded(ctx, podIP, q.Vrpq.Spec.SaveOptions.Archive, "", q.Vrpq.Spec.SaveOptions.NumRestorePoints)
	if err != nil {
		return err
	}

	// Step 2: Save the restore point and update status conditions
	return q.saveRestorePointToArchive(ctx, podIP, vapi.MainCluster)
}

// collectInfoFromVdb fetches the VerticaDB and collects communal storage access information
// required for executing restore point queries. This includes building configuration parameters
// for accessing the communal storage backend (S3, GCS, Azure, etc.).
func (q *QueryReconciler) collectInfoFromVdb(ctx context.Context) (res ctrl.Result, err error) {
	// Build communal storage configuration parameters if not already present
	// This is only needed for ShowRestorePoints queries that need direct communal access
	if q.ConfigurationParams == nil && q.Vrpq.IsShowRestorePointsQuery() {
		res, err = q.ConstructConfigParms(ctx)
		if verrors.IsReconcileAborted(res, err) {
			return res, err
		}
	}

	return res, err
}

// runShowRestorePoints calls the vclusterOps API to retrieve the list of restore points.
// It updates the VRPQ status throughout the operation lifecycle:
// - Sets "Querying" condition when starting
// - Sets "QueryComplete" condition on success/failure
// - Populates the restore points list in the status on success
func (q *QueryReconciler) runShowRestorePoints(ctx context.Context,
	opts []showrestorepoints.Option) (err error) {
	// Update status to indicate query has started
	err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Querying, metav1.ConditionTrue, "Started")}, stateQuerying, nil)
	if err != nil {
		return err
	}

	// Execute the show restore points operation via vclusterops
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsStarted,
		"Starting show restore points")
	start := time.Now()
	restorePoints, errRun := q.Dispatcher.ShowRestorePoints(ctx, opts...)
	if errRun != nil {
		if q.Vrpq.ShouldRetryOnFailure() {
			q.VRec.Eventf(q.Vrpq, corev1.EventTypeWarning, events.ShowRestorePointsFailed,
				"Show restore points failed, will retry based on annotation settings")
			return errRun
		}
		// Handle failure: emit event and update status conditions
		q.VRec.Event(q.Vrpq, corev1.EventTypeWarning, events.ShowRestorePointsFailed, "Failed when calling show restore points")
		err = vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Querying, metav1.ConditionFalse, "Failed"),
				vapi.MakeCondition(v1beta1.QueryComplete, metav1.ConditionTrue, "Failed")}, stateFailedQuery, nil)
		if err != nil {
			errRun = errors.Join(errRun, err)
		}
		return errRun
	}
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.ShowRestorePointsSucceeded,
		"Successfully queried restore points in %s", time.Since(start).Truncate(time.Second))

	// Update status to indicate successful completion and populate the restore points result
	return vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Querying, metav1.ConditionFalse, "Completed"),
			vapi.MakeCondition(v1beta1.QueryComplete, metav1.ConditionTrue, "Completed")}, stateSuccessQuery, restorePoints)
}

// createArchiveIfNeeded attempts to create an archive with the specified configuration.
// This is an idempotent operation - if the archive already exists, it returns successfully
// without error. The archive is created with optional limits on restore points and can
// target a specific sandbox.
func (q *QueryReconciler) createArchiveIfNeeded(ctx context.Context,
	initiatorIP string, archiveName string, sandbox string, numRestorePoints int) error {
	opts := q.buildCreateArchiveOptions(initiatorIP, archiveName, numRestorePoints, sandbox)
	q.VRec.Event(q.Vrpq, corev1.EventTypeNormal, events.CreateArchiveStart, "Starting create archive")
	start := time.Now()

	// Attempt to create the archive
	err := q.Dispatcher.CreateArchive(ctx, opts...)
	if err != nil {
		// If archive already exists, treat as success (idempotent operation)
		// TODO: This will be replaced by a vproblem in VER-96975
		if strings.Contains(err.Error(), "Duplicate object on host") {
			q.VRec.Event(q.Vdb, corev1.EventTypeNormal, events.ArchiveExists,
				"Attempted to create an archive that already exists")
			return nil
		}
		// For all other errors, report failure
		q.VRec.Eventf(q.Vrpq, corev1.EventTypeWarning, events.CreateArchiveFailed,
			"Failed to create archive %q", archiveName)
		return err
	}

	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.CreateArchiveSucceeded,
		"Successfully created archive. It took %s", time.Since(start).Truncate(time.Second))
	return nil
}

// saveRestorePointToArchive executes the save restore point operation and updates the VRPQ status.
// It saves a new restore point to the specified archive and records the start/end timestamps
// in the VRPQ status for tracking purposes.
func (q *QueryReconciler) saveRestorePointToArchive(ctx context.Context,
	initiatorIP, sandbox string) error {
	archiveName := q.Vrpq.Spec.SaveOptions.Archive
	opts := q.buildSaveRestorePointOptions(initiatorIP, archiveName, sandbox)
	// Update status to indicate query has started
	err := vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
		[]*metav1.Condition{vapi.MakeCondition(v1beta1.Querying, metav1.ConditionTrue, "Started")}, stateQuerying, nil)
	if err != nil {
		return err
	}
	q.VRec.Event(q.Vrpq, corev1.EventTypeNormal,
		events.SaveRestorePointStart, "Starting save restore point")
	start := time.Now()

	// Execute the save restore point operation
	errRun := q.Dispatcher.SaveRestorePoint(ctx, opts...)
	if errRun != nil {
		if q.Vrpq.ShouldRetryOnFailure() {
			q.VRec.Eventf(q.Vrpq, corev1.EventTypeWarning, events.SaveRestorePointFailed,
				"Failed to save restore point to archive: %s, will retry based on annotation settings",
				archiveName)
			return errRun
		}
		// Handle failure: emit event and update status conditions
		q.VRec.Eventf(q.Vrpq, corev1.EventTypeWarning, events.SaveRestorePointFailed,
			"Failed to save restore point to archive: %s", archiveName)
		err := vrpqstatus.Update(ctx, q.VRec.Client, q.VRec.Log, q.Vrpq,
			[]*metav1.Condition{vapi.MakeCondition(v1beta1.Querying, metav1.ConditionFalse, "Failed"),
				vapi.MakeCondition(v1beta1.QueryComplete, metav1.ConditionTrue, "Failed")}, stateFailedQuery, nil)
		if err != nil {
			errRun = errors.Join(errRun, err)
		}
		return errRun
	}
	end := time.Now()
	q.VRec.Eventf(q.Vrpq, corev1.EventTypeNormal, events.SaveRestorePointSucceeded,
		"Successfully saved restore point to archive: %s. It took %s",
		archiveName, time.Since(start).Truncate(time.Second))
	return q.updateVRPQStatusWithTimestamps(ctx, start, end)
}

// buildCreateArchiveOptions constructs the options for the create archive API call.
// It specifies the initiator pod, archive name, restore point limit, and optional sandbox target.
func (q *QueryReconciler) buildCreateArchiveOptions(initiatorIP string, archiveName string,
	numRestorePoints int, sandbox string) []createarchive.Option {
	opts := []createarchive.Option{
		createarchive.WithInitiator(initiatorIP),
		createarchive.WithArchiveName(archiveName),
		createarchive.WithNumRestorePoints(numRestorePoints),
		createarchive.WithSandbox(sandbox),
	}
	return opts
}

// buildSaveRestorePointOptions constructs the options for the save restore point API call.
// It specifies the initiator pod, target archive name, and optional sandbox.
func (q *QueryReconciler) buildSaveRestorePointOptions(initiatorIP string, archiveName string,
	sandbox string) []saverestorepoint.Option {
	opts := []saverestorepoint.Option{
		saverestorepoint.WithInitiator(initiatorIP),
		saverestorepoint.WithArchiveName(archiveName),
		saverestorepoint.WithSandbox(sandbox),
	}
	return opts
}

// findRunningPodWithNMAContainer searches for a suitable pod to execute vclusterops API calls.
// The selected pod must meet the following criteria:
// - Pod is in Running phase
// - NMA (Node Management Agent) container is ready
// - The Vertica node is UP (not down)
// Returns the pod's IP address, or empty string if no suitable pod is found.
func (q *QueryReconciler) findRunningPodWithNMAContainer(pods *corev1.PodList) string {
	for i := range pods.Items {
		pod := &pods.Items[i]
		// Check if pod is running
		if pod.Status.Phase == podRunning {
			// Check if NMA container is ready
			if vk8s.IsNMAContainerReady(pod) {
				podName := types.NamespacedName{
					Name:      pod.Name,
					Namespace: pod.Namespace,
				}
				// Verify the Vertica node is UP
				pf := q.PFacts.Detail[podName]
				if pf != nil && pf.GetUpNode() {
					return pod.Status.PodIP
				}
			}
		}
	}
	q.Log.Info("couldn't find any suitable pod to run the query")
	return ""
}

// getInitiatorPodIP identifies and returns the IP address of a suitable initiator pod.
// It searches through pods in the main cluster to find one that can execute vclusterops API calls.
// If no suitable pod is found, it requests a requeue to retry later.
func (q *QueryReconciler) getInitiatorPodIP(ctx context.Context) (string, ctrl.Result, error) {
	// Find all pods in the main cluster
	finder := iter.MakeSubclusterFinder(q.VRec.Client, q.Vdb)
	pods, err := finder.FindPods(ctx, iter.FindInVdb, vapi.MainCluster)
	if err != nil {
		return "", ctrl.Result{}, err
	}

	// Select a suitable pod with running NMA and UP node
	ip := q.findRunningPodWithNMAContainer(pods)
	if ip == "" {
		// No suitable pod available, requeue to retry
		return "", ctrl.Result{Requeue: true}, nil
	}
	return ip, ctrl.Result{}, nil
}

// updateVRPQStatusWithTimestamps updates the VRPQ status after successfully saving a restore point.
// It records the operation completion, updates status conditions, and stores the restore point
// metadata including archive name and start/end timestamps.
func (q *QueryReconciler) updateVRPQStatusWithTimestamps(ctx context.Context, start, end time.Time) error {
	refreshStatusInPlace := func(vrpq *v1beta1.VerticaRestorePointsQuery) error {
		// Mark the query as complete
		meta.SetStatusCondition(&vrpq.Status.Conditions, *vapi.MakeCondition(v1beta1.Querying, metav1.ConditionFalse, "Completed"))
		meta.SetStatusCondition(&vrpq.Status.Conditions, *vapi.MakeCondition(v1beta1.QueryComplete, metav1.ConditionTrue, "Completed"))
		vrpq.Status.State = stateSuccessQuery

		// Initialize the saved restore point info if needed
		if vrpq.Status.SavedRestorePoint == nil {
			vrpq.Status.SavedRestorePoint = new(v1beta1.RestorePointInfo)
		}

		// Record the restore point metadata
		vrpq.Status.SavedRestorePoint.Archive = q.Vrpq.Spec.SaveOptions.Archive
		vrpq.Status.SavedRestorePoint.StartTimestamp = start.Format("2006-01-02 15:04:05.000000000")
		vrpq.Status.SavedRestorePoint.EndTimestamp = end.Format("2006-01-02 15:04:05.000000000")
		return nil
	}
	// Update the VRPQ status with retry logic
	return vrpqstatus.UpdateWithRetry(ctx, q.VRec.Client, q.Log, q.Vrpq, refreshStatusInPlace)
}
