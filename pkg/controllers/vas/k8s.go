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

package vas

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/events"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

// fetchVDB will fetch the VerticaDB that is referenced in a VerticaAutoscaler.
// This will log an event if the VerticaDB is not found.
func fetchVDB(ctx context.Context, vrec *VerticaAutoscalerReconciler,
	vas *vapi.VerticaAutoscaler, vdb *vapi.VerticaDB) (ctrl.Result, error) {
	nm := types.NamespacedName{
		Namespace: vas.Namespace,
		Name:      vas.Spec.VerticaDBName,
	}
	err := vrec.Client.Get(ctx, nm, vdb)
	if err != nil && errors.IsNotFound(err) {
		vrec.EVRec.Eventf(vas, corev1.EventTypeWarning, events.VerticaDBNotFound,
			"The VerticaDB named '%s' was not found", vas.Spec.VerticaDBName)
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}
