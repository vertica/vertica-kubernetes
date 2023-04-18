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
	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	for _, ref := range r.Et.Spec.References {
		if ref.Object.Kind != vapi.VerticaDBKind || ref.Object.APIVersion != vapi.GroupVersion.String() {
			continue
		}

		vdb := &vapi.VerticaDB{}

		nm := types.NamespacedName{
			Namespace: r.Et.Namespace,
			Name:      ref.Object.Name,
		}

		if err := r.VRec.Client.Get(ctx, nm, vdb); err != nil {
			if errors.IsNotFound(err) {
				continue
			}

			return ctrl.Result{}, err
		}

		for _, match := range r.Et.Spec.Matches {
			// Grab the condition based on what was given.
			conditionType := vapi.VerticaDBConditionType(match.Condition.Type)
			conditionTypeIndex, ok := vapi.VerticaDBConditionIndexMap[conditionType]
			if !ok {
				return ctrl.Result{}, fmt.Errorf("vertica DB condition '%s' missing from VerticaDBConditionType", conditionType)
			}

			if len(vdb.Status.Conditions) <= conditionTypeIndex {
				continue
			}

			if vdb.Status.Conditions[conditionTypeIndex].Status == match.Condition.Status {
				// Check if job already created.
				if r.Et.Status.References != nil {
					continue
				}

				// Kick off the job
				job := batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name:         r.Et.Spec.Template.Metadata.Name,
						GenerateName: r.Et.Spec.Template.Metadata.GenerateName,
						Labels:       r.Et.Spec.Template.Metadata.Labels,
						Annotations:  r.Et.Spec.Template.Metadata.Annotations,
					},
					Spec: r.Et.Spec.Template.Spec,
				}

				if err := r.VRec.Client.Create(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}

				// Update status to fill in JobName,JobNamespace
				refStatus := vapi.ETRefObjectStatus{
					APIVersion:      ref.Object.APIVersion,
					Namespace:       ref.Object.Namespace,
					Name:            ref.Object.Name,
					Kind:            ref.Object.Kind,
					ResourceVersion: r.Et.ResourceVersion,
					UID:             string(r.Et.GetUID()),
					JobNamespace:    r.Et.Namespace,
					JobName:         job.Name,
				}
				if err := etstatus.Apply(ctx, r.VRec.Client, r.Et, &refStatus); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				r.VRec.Log.Info(
					"status was not met",
					"expected", match.Condition.Status,
					"found", vdb.Status.Conditions[conditionTypeIndex].Status,
				)
			}
		}
	}

	return ctrl.Result{}, nil
}
