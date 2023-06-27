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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartDB will start a subset of nodes. Use this when vertica has lost
// cluster quorum. The IP given for each vnode *must* match the current IP
// in the vertica catalog. If they aren't a call to ReIP is necessary.
func (v *VClusterOps) StartDB(ctx context.Context, opts ...startdb.Option) (ctrl.Result, error) {
	s := startdb.Parms{}
	s.Make(opts...)
	return ctrl.Result{}, fmt.Errorf("not implemented")
}
