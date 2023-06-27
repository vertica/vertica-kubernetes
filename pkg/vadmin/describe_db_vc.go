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

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/describedb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// DescribeDB will get information about a database from communal storage.
func (v *VClusterOps) DescribeDB(ctx context.Context, opts ...describedb.Option) (string, ctrl.Result, error) {
	v.Log.Info("Starting vcluster DescribeDB")
	return "", ctrl.Result{}, fmt.Errorf("not implemented")
}
