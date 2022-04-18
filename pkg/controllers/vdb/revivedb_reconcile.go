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
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/cloud"
	"github.com/vertica/vertica-kubernetes/pkg/cmds"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/paths"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReviveDBReconciler will revive a database if one doesn't exist in the vdb yet.
type ReviveDBReconciler struct {
	VRec    *VerticaDBReconciler
	Log     logr.Logger
	Vdb     *vapi.VerticaDB // Vdb is the CRD we are acting on.
	PRunner cmds.PodRunner
	PFacts  *PodFacts
}

// MakeReviveDBReconciler will build a ReviveDBReconciler object
func MakeReviveDBReconciler(vdbrecon *VerticaDBReconciler, log logr.Logger,
	vdb *vapi.VerticaDB, prunner cmds.PodRunner, pfacts *PodFacts) controllers.ReconcileActor {
	return &ReviveDBReconciler{VRec: vdbrecon, Log: log, Vdb: vdb, PRunner: prunner, PFacts: pfacts}
}

// Reconcile will ensure a DB exists and revive one if it doesn't
func (r *ReviveDBReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	// Skip this reconciler entirely if the init policy is to create the DB.
	if r.Vdb.Spec.InitPolicy != vapi.CommunalInitPolicyRevive {
		return ctrl.Result{}, nil
	}

	// The remaining revive_db logic is driven from GenericDatabaseInitializer.
	// This exists to creation an abstraction that is common with create_db.
	g := GenericDatabaseInitializer{
		initializer: r,
		VRec:        r.VRec,
		Log:         r.Log,
		Vdb:         r.Vdb,
		PRunner:     r.PRunner,
		PFacts:      r.PFacts,
	}
	return g.checkAndRunInit(ctx)
}

// execCmd will do the actual execution of admintools -t revive_db.
// This handles logging of necessary events.
func (r *ReviveDBReconciler) execCmd(ctx context.Context, atPod types.NamespacedName, cmd []string) (ctrl.Result, error) {
	r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeNormal, events.ReviveDBStart,
		"Calling 'admintools -t revive_db'")
	start := time.Now()
	stdout, _, err := r.PRunner.ExecAdmintools(ctx, atPod, names.ServerContainer, cmd...)
	if err != nil {
		switch {
		case isClusterLeaseNotExpired(stdout):
			r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.ReviveDBClusterInUse,
				"revive_db failed because the cluster lease has not expired for '%s'",
				r.Vdb.GetCommunalPath())
			return ctrl.Result{Requeue: true}, nil

		case cloud.IsBucketNotExistError(stdout):
			r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.S3BucketDoesNotExist,
				"The bucket in the S3 path '%s' does not exist", r.Vdb.GetCommunalPath())
			return ctrl.Result{Requeue: true}, nil

		case cloud.IsEndpointBadError(stdout):
			r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.S3EndpointIssue,
				"Unable to connect to S3 endpoint '%s'", r.Vdb.Spec.Communal.Endpoint)
			return ctrl.Result{Requeue: true}, nil

		case isDatabaseNotFound(stdout):
			r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.ReviveDBNotFound,
				"revive_db failed because the database '%s' could not be found in the communal path '%s'",
				r.Vdb.Spec.DBName, r.Vdb.GetCommunalPath())
			return ctrl.Result{Requeue: true}, nil

		case isPermissionDeniedError(stdout):
			r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.ReviveDBPermissionDenied,
				"revive_db failed because of a permission denied error.  Verify these paths match the "+
					"ones used by the database: %s, %s",
				r.Vdb.Spec.Local.DataPath, r.Vdb.Spec.Local.DepotPath)
			return ctrl.Result{Requeue: true}, nil

		case isNodeCountMismatch(stdout):
			r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.ReviveDBNodeCountMismatch,
				"revive_db failed because of a node count mismatch")
			return ctrl.Result{Requeue: true}, nil

		case isKerberosAuthError(stdout):
			r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.KerberosAuthError,
				"Error during keberos authentication")
			return ctrl.Result{Requeue: true}, nil

		default:
			r.VRec.EVRec.Event(r.Vdb, corev1.EventTypeWarning, events.ReviveDBFailed,
				"Failed to revive the database")
			return ctrl.Result{}, err
		}
	}
	r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeNormal, events.ReviveDBSucceeded,
		"Successfully revived database. It took %s", time.Since(start))
	return ctrl.Result{}, nil
}

func isClusterLeaseNotExpired(op string) bool {
	// We use (?s) so that '.' matches newline characters
	rs := `(?s)the communal storage location.*might still be in use.*cluster lease will expire`
	re := regexp.MustCompile(rs)
	return re.FindAllString(op, -1) != nil
}

func isDatabaseNotFound(op string) bool {
	rs := `(?s)Could not copy file.+: ` +
		`(No such file or directory|.*FileNotFoundException|File not found|.*blob does not exist)`
	re := regexp.MustCompile(rs)
	return re.FindAllString(op, -1) != nil
}

func isPermissionDeniedError(op string) bool {
	return strings.Contains(op, "Permission Denied")
}

func isNodeCountMismatch(op string) bool {
	if strings.Contains(op, "Error: Node count mismatch") {
		return true
	}
	return strings.Contains(op, "Error: Primary node count mismatch")
}

// preCmdSetup is a no-op for revive.  This exists so that we can use the
// DatabaseInitializer interface.
func (r *ReviveDBReconciler) preCmdSetup(ctx context.Context, atPod types.NamespacedName) error {
	return nil
}

// getPodList gets a list of the pods we are going to use in revive db.
// If it was not able to generate a list, possibly due to a bad reviveOrder, it
// return false for the bool return value.
func (r *ReviveDBReconciler) getPodList() ([]*PodFact, bool) {
	// Build up a map that keeps track of the number of pods for each subcluster.
	// Entries in this map get decremented as we add pods to the pod list.
	scPodCounts := map[int]int32{}
	for i := range r.Vdb.Spec.Subclusters {
		scPodCounts[i] = r.Vdb.Spec.Subclusters[i].Size
	}

	// Helper to log an event when reviveOrder is found to be bad
	logBadReviveOrder := func(reason string) {
		r.VRec.EVRec.Eventf(r.Vdb, corev1.EventTypeWarning, events.ReviveOrderBad,
			"revive_db failed because the reviveOrder specified is bad: %s", reason)
	}

	// This is the pod list that we are going to create and return
	podList := []*PodFact{}

	// Helper to add pods to podList
	addPodsFromSubcluster := func(subclusterIndex int, podsToAdd int32) bool {
		sc := &r.Vdb.Spec.Subclusters[subclusterIndex]
		for j := int32(0); j < podsToAdd; j++ {
			podsLeft := scPodCounts[subclusterIndex]
			podIndex := sc.Size - podsLeft
			pn := names.GenPodName(r.Vdb, sc, podIndex)
			pf, ok := r.PFacts.Detail[pn]
			if !ok {
				logBadReviveOrder(fmt.Sprintf("pod '%s' not found", pn.Name))
				return false
			}
			podList = append(podList, pf)
			scPodCounts[subclusterIndex]--
		}
		return true
	}

	// Start building the pod list from the revive order
	for i := range r.Vdb.Spec.ReviveOrder {
		cur := r.Vdb.Spec.ReviveOrder[i]

		if cur.SubclusterIndex < 0 || cur.SubclusterIndex >= len(r.Vdb.Spec.Subclusters) {
			logBadReviveOrder(fmt.Sprintf("subcluster index '%d' out of bounds", cur.SubclusterIndex))
			return nil, false
		}

		podsToAdd := int32(cur.PodCount)
		podsLeft := scPodCounts[cur.SubclusterIndex]
		if podsLeft < podsToAdd || podsToAdd <= 0 {
			podsToAdd = podsLeft
		}

		if !addPodsFromSubcluster(cur.SubclusterIndex, podsToAdd) {
			return nil, false
		}
	}

	// Ensure we did not miss any pods.  This loop exists mainly for the case
	// where the reviveOrder is empty.
	for i := range r.Vdb.Spec.Subclusters {
		if !addPodsFromSubcluster(i, scPodCounts[i]) {
			return nil, false
		}
	}
	return podList, true
}

// genCmd will return the command to run in the pod to create the database
func (r *ReviveDBReconciler) genCmd(ctx context.Context, hostList []string) ([]string, error) {
	cmd := []string{
		"-t", "revive_db",
		"--hosts=" + strings.Join(hostList, ","),
		"--communal-storage-location=" + r.Vdb.GetCommunalPath(),
		"--communal-storage-params=" + paths.AuthParmsFile,
		"--database", r.Vdb.Spec.DBName,
	}
	if r.Vdb.Spec.IgnoreClusterLease {
		cmd = append(cmd, "--ignore-cluster-lease")
	}
	return cmd, nil
}
