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

package errors

import (
	ctrl "sigs.k8s.io/controller-runtime"
)

// IsReconcileAborted checks if the reconcile function returned an error,
// or if a requeue is necessary
// or if the requeueAfter is set
func IsReconcileAborted(res ctrl.Result, err error) bool {
	if res.Requeue || err != nil || res.RequeueAfter > 0 {
		return true
	}
	return false
}
