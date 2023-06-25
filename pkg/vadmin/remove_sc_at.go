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

	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/removesc"
)

// RemoveSubcluster will remove the given subcluster from the vertica cluster.
func (a Admintools) RemoveSubcluster(ctx context.Context, opts ...removesc.Option) error {
	s := removesc.Parms{}
	s.Make(opts...)
	cmd := []string{
		"-t", "db_remove_subcluster",
		"--database", a.VDB.Spec.DBName,
		"--subcluster", s.Subcluster,
		"--noprompts",
	}
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, s.InitiatorName, names.ServerContainer, cmd...)
	if err != nil {
		if strings.Contains(stdout, "No subcluster found") {
			// Nothing to do if the subcluster is already gone.
			a.Log.Info("Attempted to remove a subcluster that was already gone", "subcluster", s.Subcluster)
			return nil
		}
	}
	return err
}
