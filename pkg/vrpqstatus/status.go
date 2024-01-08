/*
 (c) Copyright [2021-2023] Open Text.
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

package vrpqstatus

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func Update(ctx context.Context, clnt client.Client, log logr.Logger, vrpq *vapi.VerticaRestorePointsQuery,
	updateFunc func(*vapi.VerticaRestorePointsQuery) error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := types.NamespacedName{Namespace: vrpq.Namespace, Name: vrpq.Name}
		if err := clnt.Get(ctx, nm, vrpq); err != nil {
			log.Info("VerticaRestorePointsQuery resource not found.  Ignoring since object must be deleted")

			return err
		}
		// We will calculate the status for the vrpq object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vrpqChg := vrpq.DeepCopy()
		// Refresh the status using the users provided function
		if err := updateFunc(vrpqChg); err != nil {
			return err
		}
		if !reflect.DeepEqual(vrpq.Status, vrpqChg.Status) {
			log.Info("Updating vrpq status", "status", vrpq.Status)
			vrpqChg.Status.DeepCopyInto(&vrpq.Status)
			if err := clnt.Status().Update(ctx, vrpq); err != nil {
				return err
			}
		}
		return nil
	})
}

func UpdateCondition(ctx context.Context, clnt client.Client, log logr.Logger,
	vrpq *vapi.VerticaRestorePointsQuery, condition vapi.VerticaRestorePointsQueryCondition) error {
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}
	// refreshConditionInPlace will update the status condition in vrpq.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vrpq *vapi.VerticaRestorePointsQuery) error {
		inx, ok := vapi.VerticaRestorePointsQueryConditionIndexMap[condition.Type]
		if !ok {
			return fmt.Errorf("vertica condition '%s' missing from VerticaRestorePointsQueryType", condition.Type)
		}
		// Ensure the array is big enough
		for i := len(vrpq.Status.Conditions); i <= inx; i++ {
			vrpq.Status.Conditions = append(vrpq.Status.Conditions, vapi.VerticaRestorePointsQueryCondition{
				Type:               vapi.VerticaRestorePointsQueryConditionNameMap[i],
				Status:             corev1.ConditionFalse,
				LastTransitionTime: metav1.Unix(0, 0),
			})
		}
		// Only update if status is different change.  Cannot compare the entire
		// condition since LastTransitionTime will be different each time.
		if vrpq.Status.Conditions[inx].Status != condition.Status {
			vrpq.Status.Conditions[inx] = condition
		}
		return nil
	}
	return Update(ctx, clnt, log, vrpq, refreshConditionInPlace)
}
