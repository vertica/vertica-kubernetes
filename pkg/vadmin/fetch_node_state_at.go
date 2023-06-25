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

	"github.com/vertica/vertica-kubernetes/pkg/events"
	"github.com/vertica/vertica-kubernetes/pkg/vadmin/opts/fetchnodestate"
	ctrl "sigs.k8s.io/controller-runtime"
)

// FetchNodeState will determine if the given set of nodes are considered UP
// or DOWN in our consensus state. It returns a map of vnode and its node state.
func (a Admintools) FetchNodeState(ctx context.Context, opts ...fetchnodestate.Option) (map[string]string, ctrl.Result, error) {
	s := fetchnodestate.Parms{}
	if err := s.Make(opts...); err != nil {
		return nil, ctrl.Result{}, err
	}
	cmd := []string{
		"-t", "list_allnodes",
	}
	stdout, err := a.execAdmintools(ctx, s.Initiator, cmd...)
	if err != nil {
		res, err2 := a.logFailure("list_allnodes", events.MgmtFailed, stdout, err)
		return nil, res, err2
	}
	return a.parseClusterNodeStatus(stdout, s.HostsNeeded), ctrl.Result{}, nil
}

// parseClusterNodeStatus will parse the output from a AT -t list_allnodes call
func (a Admintools) parseClusterNodeStatus(stdout string, hostsNeeded map[string]bool) map[string]string {
	stateMap := map[string]string{}
	lines := strings.Split(stdout, "\n")
	const HeaderRowCount = 2
	if len(lines) <= HeaderRowCount {
		// Nothing to parse, return empty map
		return stateMap
	}
	// We skip the first two lines because they are for the header of the
	// output. The output that we are omitting looks like this:
	//  Node          | Host       | State | Version                 | DB
	// ---------------+------------+-------+-------------------------+----
	for _, line := range lines[HeaderRowCount:] {
		// Line is something like this:
		//   v_db_node0001 | 10.244.1.6 | UP    | vertica-11.0.0.20210309 | db
		cols := strings.Split(line, "|")
		const ListNodesColCount = 4
		if len(cols) < ListNodesColCount {
			continue
		}
		vnode := strings.Trim(cols[0], " ")
		state := strings.Trim(cols[2], " ")

		// Only include state for this host if it was requested by the caller.
		if _, ok := hostsNeeded[vnode]; ok {
			stateMap[vnode] = state
		}
	}
	return stateMap
}
