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

	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/addsc"
)

// AddSubcluster will create a subcluster in the vertica cluster.
func (a Admintools) AddSubcluster(ctx context.Context, opts ...addsc.Option) error {
	s := addsc.Parms{}
	s.Make(opts...)
	cmd := []string{
		"-t", "db_add_subcluster",
		"--database", a.VDB.Spec.DBName,
		"--subcluster", s.Subcluster,
	}

	// In v11, when adding a subcluster it defaults to a secondary.  Prior
	// versions default to a primary.  Use the correct switch, depending on what
	// version we are using.
	vinf, ok := a.VDB.MakeVersionInfo()
	const DefaultSecondarySubclusterCreationVersion = "v11.0.0"
	if ok && vinf.IsEqualOrNewer(DefaultSecondarySubclusterCreationVersion) {
		if s.IsPrimary {
			cmd = append(cmd, "--is-primary")
		}
	} else {
		if !s.IsPrimary {
			cmd = append(cmd, "--is-secondary")
		}
	}

	_, _, err := a.PRunner.ExecAdmintools(ctx, s.InitiatorName, names.ServerContainer, cmd...)
	return err
}
