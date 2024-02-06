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
	"reflect"

	"github.com/go-logr/logr"
	"github.com/vertica/vcluster/vclusterops"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
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
		err := clnt.Get(ctx, nm, vrpq)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("VerticaRestorePointsQuery resource not found.  Ignoring since object must be deleted")
				return nil
			}
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

// UpdateConditionAndState will update a condition and state status
// This is a no-op if the status condition is already set.  The input vrpq will
// be updated with the status condition.
func UpdateConditionAndState(ctx context.Context, clnt client.Client, log logr.Logger,
	vrpq *vapi.VerticaRestorePointsQuery, condition *metav1.Condition, state string) error {
	// refreshConditionInPlace will update the status condition in vrpq.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vrpq *vapi.VerticaRestorePointsQuery) error {
		if vrpq.Status.State != state {
			vrpq.Status.State = state
		}
		meta.SetStatusCondition(&vrpq.Status.Conditions, *condition)
		return nil
	}
	return Update(ctx, clnt, log, vrpq, refreshConditionInPlace)
}

// UpdateRestorePointStatus will update the restore points status. The input vrpq
// will be updated with restore points
func UpdateRestorePointStatus(ctx context.Context, clnt client.Client, log logr.Logger,
	vrpq *vapi.VerticaRestorePointsQuery, restorePoints []vclusterops.RestorePoint) error {
	return Update(ctx, clnt, log, vrpq, func(vrpq *vapi.VerticaRestorePointsQuery) error {
		if len(vrpq.Status.RestorePoints) < len(restorePoints) {
			vrpq.Status.RestorePoints = make([]vapi.RestorePoint, len(restorePoints))
		}
		for i := range restorePoints {
			vrpq.Status.RestorePoints[i].Archive = restorePoints[i].Archive
			vrpq.Status.RestorePoints[i].ID = restorePoints[i].ID
			vrpq.Status.RestorePoints[i].Index = restorePoints[i].Index
			vrpq.Status.RestorePoints[i].Timestamp = restorePoints[i].Timestamp
		}
		return nil
	})
}
