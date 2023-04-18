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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CreateETReconciler struct {
	VRec *EventTriggerReconciler
	Et   *vapi.EventTrigger
}

func MakeCreateETReconciler(r *EventTriggerReconciler, et *vapi.EventTrigger) controllers.ReconcileActor {
	return &CreateETReconciler{VRec: r, Et: et}
}

func (r *CreateETReconciler) Reconcile(ctx context.Context, req *reconcile.Request) (reconcile.Result, error) {
	for _, reference := range r.Et.Spec.References {
		if reference.Object.Kind != "VerticaDB" {
			err := fmt.Errorf("unexpected type: %s", reference.Object.Kind)
			r.VRec.Log.Error(err, "checking for reference")
			return ctrl.Result{}, err
		}

		vdb := &vapi.VerticaDB{}

		nm := types.NamespacedName{
			Namespace: r.Et.Namespace,
			Name:      reference.Object.Name,
		}

		if err := r.VRec.Client.Get(ctx, nm, vdb); err != nil {
			if errors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		if len(vdb.Status.Conditions) <= vapi.DBInitializedIndex {
			return ctrl.Result{}, nil
		}

		for _, match := range r.Et.Spec.Matches {
			if vdb.Status.Conditions[vapi.DBInitializedIndex].Status == match.Condition.Status {
				// Check if job already created.
				if r.Et.Status.References != nil {
					return ctrl.Result{}, nil
				}

				job := batchv1.Job{
					ObjectMeta: metav1.ObjectMeta{
						Name: r.Et.Spec.Template.Metadata.Name,
					},
					Spec: r.Et.Spec.Template.Spec,
				}

				if err := r.VRec.Client.Create(ctx, &job); err != nil {
					return ctrl.Result{}, err
				}

				// Update status to fill in JobName,JobNamespace
				refStatus := vapi.ETRefObjectStatus{
					Kind:         reference.Object.Kind,
					JobNamespace: r.Et.Namespace,
					JobName:      job.Name,
				}
				if err := etstatus.Apply(ctx, r.VRec.Client, r.Et, &refStatus); err != nil {
					return ctrl.Result{}, err
				}
			} else {
				r.VRec.Log.Info(
					"status was not met",
					"expected", match.Condition.Status,
					"found", vdb.Status.Conditions[vapi.DBInitializedIndex].Status,
				)
			}
		}
	}

	return ctrl.Result{}, nil
}
