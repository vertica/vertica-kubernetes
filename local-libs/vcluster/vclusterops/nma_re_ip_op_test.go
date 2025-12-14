/*
 (c) Copyright [2023-2025] Open Text.
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

package vclusterops

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type quorumTestCase struct {
	name             string
	ksafety          int
	primaryNodeCount uint
	hostCount        uint
	nodes            []nmaVNode
	expectedResult   bool
	description      string
}

func createTestNodes(specs []struct {
	name      string
	isPrimary bool
}) []nmaVNode {
	nodes := make([]nmaVNode, len(specs))
	for i, spec := range specs {
		nodes[i] = nmaVNode{Name: spec.name, IsPrimary: spec.isPrimary}
	}
	return nodes
}

func runQuorumTest(t *testing.T, tc *quorumTestCase, logger vlog.Printer) {
	op := nmaReIPOp{
		opBase:           opBase{name: "TestReIPOp", logger: logger},
		ksafety:          tc.ksafety,
		primaryNodeCount: tc.primaryNodeCount,
	}

	execContext := &opEngineExecContext{
		nmaVDatabase: nmaVDatabase{
			Nodes: tc.nodes,
		},
	}

	result := op.hasQuorumForReIP(tc.hostCount, execContext)
	assert.Equal(t, tc.expectedResult, result, tc.description)
}

func TestHasQuorumForReIP(t *testing.T) {
	logger := vlog.Printer{}
	tests := []quorumTestCase{
		{"ksafety_not_zero_quorum_passes", 1, 3, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}, {"node2", true}, {"node3", false}}), true, "ksafety != 0 should pass with quorum satisfied"},
		{"ksafety_not_zero_quorum_fails", 1, 6, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}, {"node2", true}, {"node3", false}}), false, "ksafety != 0 should fail when quorum not satisfied"},
		{"ksafety_zero_all_nodes", 0, 3, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}, {"node2", true}, {"node3", true}}), true, "ksafety == 0 should pass when all primary nodes updated"},
		{"ksafety_zero_partial_nodes", 0, 3, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}, {"node2", true}, {"node4", false}}), false, "ksafety == 0 should fail with partial nodes"},
		{"ksafety_zero_quorum_fails", 0, 6, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}, {"node2", true}, {"node3", true}, {"node4", true}, {"node5", true}, {"node6", true}}),
			false, "ksafety == 0 should fail when quorum not satisfied"},
		{"ksafety_zero_single_node", 0, 1, 1, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"node1", true}}), true, "ksafety == 0 with single node should pass"},
		{"ksafety_zero_empty_database", 0, 3, 2, []nmaVNode{}, false, "ksafety == 0 should fail when nmaVDatabase is empty"},
		{"ksafety_zero_secondary_only", 0, 2, 2, createTestNodes([]struct {
			name      string
			isPrimary bool
		}{{"sec1", false}, {"sec2", false}}), false, "ksafety == 0 should fail with only secondary nodes"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			runQuorumTest(t, &tc, logger)
		})
	}
}
