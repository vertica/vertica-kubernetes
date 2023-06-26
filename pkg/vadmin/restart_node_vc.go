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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/restartnode"
	ctrl "sigs.k8s.io/controller-runtime"
)

// RestartNode will restart a subset of nodes. Use this when vertica has not
// lost cluster quorum. The IP given for each vnode may not match the current IP
// in the vertica catalogs.
func (v VClusterOps) RestartNode(ctx context.Context, opts ...restartnode.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster RestartNode")
	s := restartnode.Parms{}
	s.Make(opts...)
	return ctrl.Result{}, fmt.Errorf("not implemented")
}
