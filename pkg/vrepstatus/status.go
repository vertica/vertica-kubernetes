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

package vrepstatus

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func updateImpl(ctx context.Context, clnt client.Client, log logr.Logger, vrep *vapi.VerticaReplicator,
	updateFunc func(*vapi.VerticaReplicator) error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := types.NamespacedName{Namespace: vrep.Namespace, Name: vrep.Name}
		err := clnt.Get(ctx, nm, vrep)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("VerticaReplicator resource not found.  Ignoring since object must be deleted")
				return nil
			}
			return err
		}
		// We will calculate the status for the vrep object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vrepChg := vrep.DeepCopy()
		// Refresh the status using the users provided function
		if err := updateFunc(vrepChg); err != nil {
			return err
		}
		if !reflect.DeepEqual(vrep.Status, vrepChg.Status) {
			log.Info("Updating vrep status", "status", vrep.Status)
			vrepChg.Status.DeepCopyInto(&vrep.Status)
			if err := clnt.Status().Update(ctx, vrep); err != nil {
				return err
			}
		}
		return nil
	})
}

// Update will update a condition and state status
// This is a no-op if the status condition is already set. The input vrep will
// be updated with the status condition.
func Update(ctx context.Context, clnt client.Client, log logr.Logger,
	vrep *vapi.VerticaReplicator, conditions []*metav1.Condition, state string, transactionID int64) error {
	refreshConditionInPlace := func(vrep *vapi.VerticaReplicator) error {
		// refreshConditionInPlace will update the status condition, state
		// in vrep. The update will be applied in-place.
		if vrep.Status.State != state {
			vrep.Status.State = state
		}
		if vrep.Status.TransactionID != transactionID {
			vrep.Status.TransactionID = transactionID
		}
		for _, condition := range conditions {
			meta.SetStatusCondition(&vrep.Status.Conditions, *condition)
		}
		return nil
	}
	return updateImpl(ctx, clnt, log, vrep, refreshConditionInPlace)
}

// Reset will clear the status state and conditions of a vrep
func Reset(ctx context.Context, clnt client.Client, log logr.Logger,
	vrep *vapi.VerticaReplicator) error {
	resetStateAndConditionInPlace := func(vrep *vapi.VerticaReplicator) error {
		vrep.Status.State = ""
		vrep.Status.Conditions = []metav1.Condition{}
		return nil
	}
	return updateImpl(ctx, clnt, log, vrep, resetStateAndConditionInPlace)
}
