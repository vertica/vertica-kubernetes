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

package vasstatus

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"github.com/vertica/vertica-kubernetes/pkg/builder"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetSelector(ctx context.Context, c client.Client, log logr.Logger, req *ctrl.Request) error {
	return vasStatusUpdater(ctx, c, log, req, func(vas *vapi.VerticaAutoscaler) {
		vas.Status.Selector = getLabelSelector(vas)
	})
}

// ReportScalingOperation bumps up the count in the status field about the number of
// times we have scaled the VerticaDB.  This is intended to be called each time
// we change the pod count up or down.
func ReportScalingOperation(ctx context.Context, c client.Client, log logr.Logger, req *ctrl.Request, currentSize int32) error {
	return vasStatusUpdater(ctx, c, log, req, func(vas *vapi.VerticaAutoscaler) {
		vas.Status.ScalingCount++
		vas.Status.CurrentSize = currentSize
	})
}

// RefreshCurrentSize sets the current size in the VerticaAutoscaler
func RefreshCurrentSize(ctx context.Context, c client.Client, log logr.Logger, req *ctrl.Request, currentSize int32) error {
	return vasStatusUpdater(ctx, c, log, req, func(vas *vapi.VerticaAutoscaler) {
		vas.Status.CurrentSize = currentSize
	})
}

// UpdateCondition will update a condition status.  This is a no-op if the
// status condition is already set.
func UpdateCondition(ctx context.Context, clnt client.Client, log logr.Logger,
	req *ctrl.Request, condition vapi.VerticaAutoscalerCondition) error {
	if condition.LastTransitionTime.IsZero() {
		condition.LastTransitionTime = metav1.Now()
	}
	// refreshConditionInPlace will update the status condition in vdb.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vas *vapi.VerticaAutoscaler) {
		if len(vas.Status.Conditions) == 0 {
			vas.Status.Conditions = append(vas.Status.Conditions, vapi.VerticaAutoscalerCondition{})
		}
		// Only update if status is different change.  Cannot compare the entire
		// condition since LastTransitionTime will be different each time.
		if vas.Status.Conditions[vapi.TargetSizeInitializedIndex].Status != condition.Status {
			vas.Status.Conditions[vapi.TargetSizeInitializedIndex] = condition
		}
	}

	return vasStatusUpdater(ctx, clnt, log, req, refreshConditionInPlace)
}

func vasStatusUpdater(ctx context.Context, c client.Client, log logr.Logger,
	req *ctrl.Request, statusUpdateFunc func(*vapi.VerticaAutoscaler)) error {
	// Try the status update in a retry loop to handle the case where someone
	// update the VerticaAutoscaler since we last fetched.
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		vas := &vapi.VerticaAutoscaler{}
		err := c.Get(ctx, req.NamespacedName, vas)
		if err != nil {
			if errors.IsNotFound(err) {
				log.Info("VerticaAutoscaler resource not found.  Ignoring since object must be deleted")
				return nil
			}
			return err
		}

		// We will calculate the status for the vas object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vasOrig := vas.DeepCopy()

		statusUpdateFunc(vas)

		if !reflect.DeepEqual(vasOrig, vas.Status) {
			log.Info("Updating vas status", "status", vas.Status)
			if err := c.Status().Update(ctx, vas); err != nil {
				return err
			}
		}
		return nil
	})
}

// getLabelSelector will generate the label for use in the vas status field
func getLabelSelector(vas *vapi.VerticaAutoscaler) string {
	return fmt.Sprintf("%s=%s,%s=%s,%s=%s",
		builder.SubclusterSvcNameLabel,
		vas.Spec.ServiceName,
		builder.VDBInstanceLabel,
		vas.Spec.VerticaDBName,
		builder.ManagedByLabel,
		builder.OperatorName,
	)
}
