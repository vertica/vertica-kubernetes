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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
)

// RemoveSubcluster will remove the given subcluster from the vertica cluster.
func (v *VClusterOps) RemoveSubcluster(_ context.Context, opts ...removesc.Option) error {
	v.Log.Info("Starting vcluster RemoveSubcluster")
	s := removesc.Parms{}
	s.Make(opts...)
	return fmt.Errorf("not implemented")
}
