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

package vscrstatus

import (
	"context"
	"reflect"

	"github.com/vertica/vertica-kubernetes/api/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func update(ctx context.Context, clnt client.Client, vscr *v1beta1.VerticaScrutinize,
	updateFunc func(*v1beta1.VerticaScrutinize) error) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// Always fetch the latest to minimize the chance of getting a conflict error.
		nm := vscr.ExtractNamespacedName()
		if err := clnt.Get(ctx, nm, vscr); err != nil {
			return err
		}

		// We will calculate the status for the vscr object. This update is done in
		// place. If anything differs from the copy then we will do a single update.
		vscrChg := vscr.DeepCopy()

		// Refresh the status using the users provided function
		if err := updateFunc(vscrChg); err != nil {
			return err
		}

		if !reflect.DeepEqual(vscr.Status, vscrChg.Status) {
			vscrChg.Status.DeepCopyInto(&vscr.Status)
			if err := clnt.Status().Update(ctx, vscr); err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateStatus updates vertica scrutinize status. The input vscr's status will
// be updated with the values from the VerticaScrutinizeStatus object
func UpdateStatus(ctx context.Context, clnt client.Client, vscr *v1beta1.VerticaScrutinize,
	vscrChgStatus *v1beta1.VerticaScrutinizeStatus) error {
	// refreshStatus will update the status in vscr.  The update
	// will be applied in-place.
	refreshStatus := func(vscr *v1beta1.VerticaScrutinize) error {
		if vscr.Status.PodName != vscrChgStatus.PodName {
			vscr.Status.PodName = vscrChgStatus.PodName
		}
		if vscr.Status.PodUID != vscrChgStatus.PodUID {
			vscr.Status.PodUID = vscrChgStatus.PodUID
		}
		if vscr.Status.TarballName != vscrChgStatus.TarballName {
			vscr.Status.TarballName = vscrChgStatus.TarballName
		}
		for _, condition := range vscrChgStatus.Conditions {
			meta.SetStatusCondition(&vscr.Status.Conditions, condition)
		}
		return nil
	}

	return update(ctx, clnt, vscr, refreshStatus)
}

// UpdateCondition will update a condition status
// This is a no-op if the status condition is already set. The input vscr will
// be updated with the status condition.
func UpdateCondition(ctx context.Context, clnt client.Client, vscr *v1beta1.VerticaScrutinize, condition *metav1.Condition) error {
	// refreshConditionInPlace will update the status condition in vscr.  The update
	// will be applied in-place.
	refreshConditionInPlace := func(vscr *v1beta1.VerticaScrutinize) error {
		meta.SetStatusCondition(&vscr.Status.Conditions, *condition)
		return nil
	}

	return update(ctx, clnt, vscr, refreshConditionInPlace)
}
