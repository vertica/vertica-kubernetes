/*
 (c) Copyright [2021-2024] Open Text.
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

package catalog

import (
	"context"
	"fmt"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
)

func (v *VSQL) FetchNodeState(ctx context.Context) (*NodeInfo, error) {
	sql := v.buildFetchNodeStateQuery()
	stdout, err := v.queryNodeStatus(ctx, sql)
	if err != nil {
		// Skip parsing that happens next
		return nil, err
	}
	return parseNodeState(stdout)
}

// buildFetchNodeStateQuery constructs the query to get the node state
func (v *VSQL) buildFetchNodeStateQuery() string {
	// The first two columns are just for informational purposes.
	cols := "n.node_name, node_state"
	if v.VDB.IsEON() {
		cols = fmt.Sprintf("%s, subcluster_oid", cols)
	} else {
		cols = fmt.Sprintf("%s, ''", cols)
	}
	// The read-only state is a new state added in 11.0.2.  So we can only query
	// for it on levels 11.0.2+.  Otherwise, we always treat read-only as being
	// disabled.
	vinf, ok := v.VDB.MakeVersionInfo()
	if ok && vinf.IsEqualOrNewer(vapi.NodesHaveReadOnlyStateVersion) {
		cols = fmt.Sprintf("%s, is_readonly", cols)
	}
	if v.VDB.IsEON() && ok && vinf.IsEqualOrNewer(vapi.NodesHaveSandboxStateVersion) {
		cols = fmt.Sprintf("%s, n.sandbox", cols)
	}
	var sql string
	if v.VDB.IsEON() {
		sql = fmt.Sprintf(
			"select %s "+
				"from nodes as n, subclusters as s "+
				"where s.node_oid = n.node_id and n.node_name in (select node_name from current_session)",
			cols)
	} else {
		sql = fmt.Sprintf(
			"select %s "+
				"from nodes as n "+
				"where n.node_name in (select node_name from current_session)",
			cols)
	}
	return sql
}

// queryNodeStatus will query the nodes system table for the following info:
// node name, node is up, read-only state, subcluster oid and sandbox name.
// It assumes the database exists and the pod is running.
func (v *VSQL) queryNodeStatus(ctx context.Context, sql string) (string, error) {
	cmd := []string{"-tAc", sql}
	stdout, _, err := v.PRunner.ExecVSQL(ctx, v.PodName, v.ExecContainerName, cmd...)
	return stdout, err
}

// parseNodeState will parse query output from node state
func parseNodeState(stdout string) (*NodeInfo, error) {
	// For testing purposes we early out with no error if there is no output
	if stdout == "" {
		return nil, nil
	}
	// The stdout comes in the form like this:
	// v_vertdb_node0001|UP|41231232423|t|sandbox1
	// This means upNode is true, subcluster oid is 41231232423 readOnly is
	// true and the node is part of sandbox1. The node name is included in the output for debug purposes, but
	// otherwise not used.
	//
	// The 2nd column for node state is ignored in here. It is just for
	// informational purposes. The fact that we got something implies the node
	// was up.
	lines := strings.Split(stdout, "\n")
	cols := strings.Split(lines[0], "|")
	var err error
	const MinExpectedCols = 3
	if len(cols) < MinExpectedCols {
		err = fmt.Errorf("expected at least %d columns from node query but only got %d", MinExpectedCols, len(cols))
		return nil, err
	}
	ninf := &NodeInfo{}
	ninf.SubclusterOid = cols[2]
	// Read-only can be missing on versions that don't support that state.
	// Return false in those cases.
	if len(cols) > MinExpectedCols {
		ninf.ReadOnly = cols[3] == "t"
		// sandbox can be missing on versions that don't support that state
		if len(cols) > MinExpectedCols+1 {
			ninf.SandboxName = cols[4]
		}
	} else {
		ninf.ReadOnly = false
	}
	return ninf, nil
}
