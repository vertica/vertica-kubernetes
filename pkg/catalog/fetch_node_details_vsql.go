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
	"strconv"
	"strings"

	vapi "github.com/vertica/vertica-kubernetes/api/v1"
)

// FetchNodeDetails returns details for a node, including its state, shard subscriptions, and depot details
func (v *VSQL) FetchNodeDetails(ctx context.Context) (nodeDetails *NodeDetails, err error) {
	nodeDetails = &NodeDetails{}
	sql := v.buildFetchNodeStateQuery()
	stdout, err := v.executeSQL(ctx, sql)
	if err != nil {
		// Skip parsing that happens next
		return nil, err
	}
	err = nodeDetails.parseNodeState(stdout)
	if err != nil {
		return nil, err
	}

	sql = v.buildFetchShardSubscriptionsQuery()
	stdout, err = v.executeSQL(ctx, sql)
	if err != nil {
		// Skip parsing that happens next
		return nil, err
	}
	err = nodeDetails.parseShardSubscriptions(stdout)
	if err != nil {
		return nil, err
	}

	sql = v.buildFetchDepotDetailsQuery()
	stdout, err = v.executeSQL(ctx, sql)
	if err != nil {
		// Skip parsing that happens next
		return nil, err
	}
	err = nodeDetails.parseDepotDetails(stdout)
	if err != nil {
		return nil, err
	}

	return nodeDetails, nil
}

// buildFetchNodeStateQuery constructs a sql query to get the node state
func (v *VSQL) buildFetchNodeStateQuery() string {
	// The first two columns are just for informational purposes.
	cols := "n.node_name, node_state"
	if v.VDB.IsEON() {
		cols = fmt.Sprintf("%s, subcluster_oid", cols)
	} else {
		cols = fmt.Sprintf("%s, ''", cols)
	}
	// During read-only online upgrade, we should use the old version to determine if we
	// will append the sandbox state to the query.
	// The reason is some subclusters might use a low version that does not contain
	// the sandbox state.
	vinf, ok := v.VDB.MakeVersionInfoDuringROUpgrade()
	// The read-only state is a new state added in 11.0.2.  So we can only query
	// for it on levels 11.0.2+.  Otherwise, we always treat read-only as being
	// disabled.
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

// buildFetchShardSubscriptionsQuery constructs a sql query to get shard subscriptions
func (v *VSQL) buildFetchShardSubscriptionsQuery() string {
	return fmt.Sprintf("select count(*) from v_catalog.node_subscriptions where node_name = '%s' and shard_name != 'replica'",
		v.VNodeName)
}

// buildFetchDepotDetailsQuery constructs a sql query to get depot details
func (v *VSQL) buildFetchDepotDetailsQuery() string {
	return fmt.Sprintf("select max_size, disk_percent from storage_locations "+
		"where location_usage = 'DEPOT' and node_name = '%s'", v.VNodeName)
}

// executeSQL will run a sql query through vsql in the pod.
// It assumes the database exists and the pod is running.
func (v *VSQL) executeSQL(ctx context.Context, sql string) (string, error) {
	cmd := []string{"-tAc", sql}
	stdout, _, err := v.PRunner.ExecVSQL(ctx, v.PodName, v.ExecContainerName, cmd...)
	return stdout, err
}

// parseNodeState will parse query output from node state
func (nodeDetails *NodeDetails) parseNodeState(stdout string) error {
	// For testing purposes we early out with no error if there is no output
	if stdout == "" {
		return nil
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
		return err
	}
	nodeDetails.SubclusterOid = cols[2]
	// Read-only can be missing on versions that don't support that state.
	// Return false in those cases.
	if len(cols) > MinExpectedCols {
		nodeDetails.ReadOnly = cols[3] == "t"
		// sandbox can be missing on versions that don't support that state
		if len(cols) > MinExpectedCols+1 {
			nodeDetails.SandboxName = cols[4]
		}
	} else {
		nodeDetails.ReadOnly = false
	}
	return nil
}

// parseShardSubscriptions will parse the query output of shard subscriptions
func (nodeDetails *NodeDetails) parseShardSubscriptions(op string) error {
	// For testing purposes we early out with no error if there is no output
	if op == "" {
		return nil
	}

	lines := strings.Split(op, "\n")
	subs, err := strconv.Atoi(lines[0])
	if err != nil {
		return err
	}
	nodeDetails.ShardSubscriptions = subs
	return nil
}

// parseDepotDetails will parse the query output of depot details
func (nodeDetails *NodeDetails) parseDepotDetails(op string) error {
	// For testing purposes, return without error if there is no output
	if op == "" {
		return nil
	}
	lines := strings.Split(op, "\n")
	cols := strings.Split(lines[0], "|")
	const ExpectedCols = 2
	if len(cols) != ExpectedCols {
		return fmt.Errorf("expected %d columns from storage_locations query but only got %d", ExpectedCols, len(cols))
	}
	var err error
	nodeDetails.MaxDepotSize, err = strconv.ParseUint(cols[0], 10, 64)
	if err != nil {
		return err
	}
	nodeDetails.DepotDiskPercentSize = cols[1]
	return nil
}
