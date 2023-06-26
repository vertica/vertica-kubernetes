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
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removenode"
)

// RemoveNode will remove an existng vrtica node from the cluster.
func (a Admintools) RemoveNode(ctx context.Context, opts ...removenode.Option) error {
	s := removenode.Parms{}
	s.Make(opts...)
	cmd := []string{
		"-t", "db_remove_node",
		"--database", a.VDB.Spec.DBName,
		"--hosts", strings.Join(s.Hosts, ","),
		"--noprompts",
	}
	_, err := a.execAdmintools(ctx, s.InitiatorName, cmd...)
	return err
}
