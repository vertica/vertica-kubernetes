/*
Copyright [2021-2023] Open Text.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package et

import (
	"context"

	"github.com/go-logr/logr"
	v1api "github.com/vertica/vertica-kubernetes/api/v1"
	v1beta1api "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/etstatus"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type VerticaDBRefReconciler struct {
	VRec *EventTriggerReconciler
	Et   *v1beta1api.EventTrigger
	Log  logr.Logger
}

func MakeVerticaDBRefReconciler(r *EventTriggerReconciler, et *v1beta1api.EventTrigger, log logr.Logger) controllers.ReconcileActor {
	return &VerticaDBRefReconciler{VRec: r, Et: et, Log: log}
}

func (r *VerticaDBRefReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	for _, ref := range r.Et.Spec.References {
		if ref.Object.Kind != v1api.VerticaDBKind {
			continue
		}

		// Create the refStatus object as we need to update the status at the
		// end regardless of what happens.
		refStatus := etstatus.Fetch(r.Et, ref.Object)

		vdb := &v1api.VerticaDB{}
		nm := types.NamespacedName{Namespace: r.Et.Namespace, Name: ref.Object.Name}

		if err := r.VRec.Client.Get(ctx, nm, vdb); err != nil {
			if errors.IsNotFound(err) {
				if errs := etstatus.Apply(ctx, r.VRec.Client, r.Log, r.Et, refStatus); errs != nil {
					return ctrl.Result{}, errs
				}

				continue
			}

			return ctrl.Result{}, err
		}

		refStatus.ResourceVersion = vdb.ResourceVersion
		refStatus.UID = vdb.GetUID()

		shouldCreateJob := true
		for _, match := range r.Et.Spec.Matches {
			if !r.matchStatus(vdb, ref, match) {
				shouldCreateJob = false
				break
			}
		}

		// Check if job already created.
		if r.isJobCreated(ref) {
			shouldCreateJob = false
		}

		if shouldCreateJob {
			// Kick off the job
			job, err := r.VRec.createJob(ctx, r.Et)
			if err != nil {
				return ctrl.Result{}, err
			}
			r.Log.Info("job created", "job.Name", job.Name, "job.Namespace", job.Namespace)

			refStatus.JobNamespace = job.Namespace
			refStatus.JobName = job.Name
			refStatus.JobsCreated = 1
		}

		if err := etstatus.Apply(ctx, r.VRec.Client, r.Log, r.Et, refStatus); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// isJobCreated will traverse all the status references and return true only
// when the job is already created.
func (r *VerticaDBRefReconciler) isJobCreated(ref v1beta1api.ETReference) bool {
	for refStatusIdx := range r.Et.Status.References {
		refStatus := r.Et.Status.References[refStatusIdx]
		if refStatus.Name == ref.Object.Name &&
			refStatus.APIVersion == ref.Object.APIVersion &&
			refStatus.Kind == ref.Object.Kind &&
			refStatus.JobName != "" {
			return true
		}
	}

	return false
}

// matchStatus will check if the matching condition given from the manifest
// matches with the reference object and return false when it doesn't match.
func (r *VerticaDBRefReconciler) matchStatus(vdb *v1api.VerticaDB, ref v1beta1api.ETReference, match v1beta1api.ETMatch) bool {
	// Grab the condition based on what was given.
	cond := vdb.FindStatusCondition(match.Condition.Type)
	if cond == nil {
		r.Log.Info("condition not in vdb", "condition", match.Condition.Type)
		return false
	}

	matchStatus := metav1.ConditionStatus(match.Condition.Status)
	if cond.Status != matchStatus {
		r.Log.Info(
			"status was not met",
			"condition", cond.Type,
			"expected", matchStatus,
			"found", cond.Status,
			"refObjectName", ref.Object.Name,
		)
		return false
	}

	return true
}
