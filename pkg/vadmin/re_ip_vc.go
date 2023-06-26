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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/reip"
	ctrl "sigs.k8s.io/controller-runtime"
)

// ReIP will update the catalog on disk with new IPs for all of the nodes given.
func (v VClusterOps) ReIP(ctx context.Context, opts ...reip.Option) (ctrl.Result, error) {
	v.Log.Info("Starting vcluster ReIP")
	s := reip.Parms{}
	s.Make(opts...)
	return ctrl.Result{}, fmt.Errorf("not implemented")
}
