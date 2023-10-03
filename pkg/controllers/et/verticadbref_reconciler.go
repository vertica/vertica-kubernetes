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
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/controllers"
	"github.com/vertica/vertica-kubernetes/pkg/etstatus"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type VerticaDBRefReconciler struct {
	VRec *EventTriggerReconciler
	Et   *vapi.EventTrigger
	Log  logr.Logger
}

func MakeVerticaDBRefReconciler(r *EventTriggerReconciler, et *vapi.EventTrigger, log logr.Logger) controllers.ReconcileActor {
	return &VerticaDBRefReconciler{VRec: r, Et: et, Log: log}
}

func (r *VerticaDBRefReconciler) Reconcile(ctx context.Context, _ *ctrl.Request) (ctrl.Result, error) {
	for _, ref := range r.Et.Spec.References {
		if ref.Object.Kind != vapi.VerticaDBKind {
			continue
		}

		// Create the refStatus object as we need to update the status at the
		// end regardless of what happens.
		refStatus := etstatus.Fetch(r.Et, ref.Object)

		vdb := &vapi.VerticaDB{}
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
func (r *VerticaDBRefReconciler) isJobCreated(ref vapi.ETReference) bool {
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
func (r *VerticaDBRefReconciler) matchStatus(vdb *vapi.VerticaDB, ref vapi.ETReference, match vapi.ETMatch) bool {
	// Grab the condition based on what was given.
	conditionType := vapi.VerticaDBConditionType(match.Condition.Type)
	conditionTypeIndex, ok := vapi.VerticaDBConditionIndexMap[conditionType]
	if !ok {
		r.Log.Info("vertica DB condition missing from VerticaDBConditionType", "condition", match.Condition.Type)
		return false
	}

	if len(vdb.Status.Conditions) <= conditionTypeIndex {
		return false
	}

	if vdb.Status.Conditions[conditionTypeIndex].Status != match.Condition.Status {
		r.Log.Info(
			"status was not met",
			"expected", match.Condition.Status,
			"found", vdb.Status.Conditions[conditionTypeIndex].Status,
			"refObjectName", ref.Object.Name,
		)
		return false
	}

	return true
}
