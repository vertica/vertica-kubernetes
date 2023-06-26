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
	"strings"

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/names"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/startdb"
	ctrl "sigs.k8s.io/controller-runtime"
)

// StartDB will start a subset of nodes. Use this when vertica has lost
// cluster quorum. The IP given for each vnode *must* match the current IP
// in the vertica catalog. If they aren't a call to ReIP is necessary.
func (a Admintools) StartDB(ctx context.Context, opts ...startdb.Option) (ctrl.Result, error) {
	s := startdb.Parms{}
	s.Make(opts...)
	cmd := a.genStartDBCommand(&s)
	stdout, _, err := a.PRunner.ExecAdmintools(ctx, s.InitiatorName, names.ServerContainer, cmd...)
	if err != nil {
		return a.logFailure("start_db", events.MgmtFailed, stdout, err)
	}
	return ctrl.Result{}, nil
}

// genStartDBCommand will return the command for start_db
func (a Admintools) genStartDBCommand(s *startdb.Parms) []string {
	cmd := []string{
		"-t", "start_db",
		"--database=" + a.VDB.Spec.DBName,
		"--noprompt",
	}
	if a.VDB.Spec.IgnoreClusterLease {
		cmd = append(cmd, "--ignore-cluster-lease")
	}
	if a.VDB.Spec.RestartTimeout != 0 {
		cmd = append(cmd, fmt.Sprintf("--timeout=%d", a.VDB.Spec.RestartTimeout))
	}

	// In all versions that we support we can include a list of hosts to start.
	// This parameter becomes important for online upgrade as we use this to
	// start the primaries while the secondary are in read-only.
	cmd = append(cmd, "--hosts", strings.Join(s.Hosts, ","))

	return cmd
}
