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

package etstatus

import (
	"context"

	vapi "github.com/vertica/vertica-kubernetes/api/v1beta1"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Apply will handle updates of ETRefObjectStatus. If the object isn't already in
// the status, a new entry will be added. If an object with the same GVK+name
// exists, then it will update that in the list.
func Apply(ctx context.Context, clnt client.Client, et *vapi.EventTrigger, stat *vapi.ETRefObjectStatus) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		// We refresh the EventTrigger incase we need to do a retry. But we
		// assume it's sufficiently populated to have a name.
		nm := et.ExtractNamespacedName()
		if err := clnt.Get(ctx, nm, et); err != nil {
			return err
		}

		if et.Status.References == nil {
			et.Status.References = []vapi.ETRefObjectStatus{}
		}
		foundObj := false
		for i := range et.Status.References {
			if et.Status.References[i].IsSameObject(stat) {
				stat.DeepCopyInto(&et.Status.References[i])
				break
			}
		}
		if !foundObj {
			et.Status.References = append(et.Status.References, *stat)
		}

		return clnt.Status().Update(ctx, et)
	})
}
