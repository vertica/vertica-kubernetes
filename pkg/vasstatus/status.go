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
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func SetSelector(ctx context.Context, c client.Client, log logr.Logger, req *ctrl.Request) error {
	return vasStatusUpdater(ctx, c, log, req, func(vas *vapi.VerticaAutoscaler) {
		vas.Status.Selector = getLabelSelector(vas)
	})
}

// IncrScalingCount bumps up the count in the status field about the number of
// times we have scaled the VerticaDB.  This is intended to be called each time
// we change the pod count up or down.
func IncrScalingCount(ctx context.Context, c client.Client, log logr.Logger, req *ctrl.Request) error {
	return vasStatusUpdater(ctx, c, log, req, func(vas *vapi.VerticaAutoscaler) {
		vas.Status.ScalingCount++
	})
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
		vas.Spec.SubclusterServiceName,
		builder.VDBInstanceLabel,
		vas.Spec.VerticaDBName,
		builder.ManagedByLabel,
		builder.OperatorName,
	)
}
