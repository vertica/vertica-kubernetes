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

package vadmin

import (
	"context"
	"fmt"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	ctrl "sigs.k8s.io/controller-runtime"
)

// FetchNodeState will determine if the given set of nodes are considered UP
// or DOWN in our consensous state. It returns a map of vnode to its node state.
func (v VClusterOps) FetchNodeState(ctx context.Context, opts ...fetchnodestate.Option) (map[string]string, ctrl.Result, error) {
	v.Log.Info("Starting vcluster FetchNodeState")
	return nil, ctrl.Result{}, fmt.Errorf("not implemented")
}
