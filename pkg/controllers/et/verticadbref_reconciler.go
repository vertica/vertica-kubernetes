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
	"fmt"

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
}

func MakeVerticaDBRefReconciler(r *EventTriggerReconciler, et *vapi.EventTrigger) controllers.ReconcileActor {
	return &VerticaDBRefReconciler{VRec: r, Et: et}
}

func (r *VerticaDBRefReconciler) Reconcile(ctx context.Context, req *ctrl.Request) (ctrl.Result, error) {
	for refIdx, ref := range r.Et.Spec.References {
		if ref.Object.Kind != vapi.VerticaDBKind || ref.Object.APIVersion != vapi.GroupVersion.String() {
			continue
		}

		// Create the refStatus object as we need to update the status at the
		// end regardless of what happens.
		refStatus := vapi.ETRefObjectStatus{
			APIVersion: ref.Object.APIVersion,
			Namespace:  ref.Object.Namespace,
			Name:       ref.Object.Name,
			Kind:       ref.Object.Kind,
		}

		vdb := &vapi.VerticaDB{}
		nm := types.NamespacedName{Namespace: r.Et.Namespace, Name: ref.Object.Name}

		if err := r.VRec.Client.Get(ctx, nm, vdb); err != nil {
			if errors.IsNotFound(err) {
				if errs := etstatus.Apply(ctx, r.VRec.Client, r.Et, &refStatus); errs != nil {
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
		if len(r.Et.Status.References) > 0 && r.Et.Status.References[refIdx].JobName != "" {
			shouldCreateJob = false
		}

		if shouldCreateJob {
			// Kick off the job
			job, err := r.VRec.createJob(ctx, r.Et)
			if err != nil {
				return ctrl.Result{}, err
			}

			refStatus.JobNamespace = job.Namespace
			refStatus.JobName = job.Name
		}

		if err := etstatus.Apply(ctx, r.VRec.Client, r.Et, &refStatus); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// matchStatus will check if the matching condition given from the manifest
// matches with the reference object and return false when it doesn't match.
func (r *VerticaDBRefReconciler) matchStatus(vdb *vapi.VerticaDB, ref vapi.ETReference, match vapi.ETMatch) bool {
	// Grab the condition based on what was given.
	conditionType := vapi.VerticaDBConditionType(match.Condition.Type)
	conditionTypeIndex, ok := vapi.VerticaDBConditionIndexMap[conditionType]
	if !ok {
		r.VRec.Log.Info(fmt.Sprintf("vertica DB condition %s missing from VerticaDBConditionType", match.Condition.Type))
		return false
	}

	if len(vdb.Status.Conditions) <= conditionTypeIndex {
		return false
	}

	if vdb.Status.Conditions[conditionTypeIndex].Status != match.Condition.Status {
		r.VRec.Log.Info(
			"status was not met",
			"expected", match.Condition.Status,
			"found", vdb.Status.Conditions[conditionTypeIndex].Status,
			"refObjectName", ref.Object.Name,
		)
		return false
	}

	return true
}
