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

func TestVReturnEpochFactory(t *testing.T) {
	options := VReturnEpochFactory()

	// Check that the factory creates an instance with default values
	assert.NotNil(t, options)
	assert.IsType(t, VReturnEpochOptions{}, options)
}

func TestNewLastGoodEpoch(t *testing.T) {
	epoch := int64(12345)
	timestamp := "2025-10-20 12:00:00"

	lge := NewLastGoodEpoch(epoch, timestamp)

	assert.NotNil(t, lge)
	assert.Equal(t, epoch, lge.lastGoodEpoch)
	assert.Equal(t, timestamp, lge.lastTimestamp)
	assert.Equal(t, 1, lge.counter)
}

func TestGetNodeInfoForEpoch(t *testing.T) {
	vdb := &VCoordinationDatabase{
		HostNodeMap: map[string]*VCoordinationNode{
			"192.168.1.101": {
				Name:        "node1",
				Address:     "192.168.1.101",
				CatalogPath: "/data/test_db/v_test_db_node0001_catalog",
			},
			"192.168.1.102": {
				Name:        "node2",
				Address:     "192.168.1.102",
				CatalogPath: "/data/test_db/v_test_db_node0002_catalog",
			},
		},
	}

	hosts := []string{"192.168.1.101", "192.168.1.102"}
	hostCatPathMap, err := buildHostCatalogPathMap(hosts, vdb)

	assert.NoError(t, err)
	assert.Equal(t, "/data/test_db/v_test_db_node0001_catalog", hostCatPathMap["192.168.1.101"])
	assert.Equal(t, "/data/test_db/v_test_db_node0002_catalog", hostCatPathMap["192.168.1.102"])

	// Test with missing host info
	hosts = []string{"192.168.1.101", "192.168.1.103"} // 103 not in VDB
	_, err = buildHostCatalogPathMap(hosts, vdb)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "host 192.168.1.103 has no saved info")

	// Test with empty node name
	vdb.HostNodeMap["192.168.1.101"].Name = ""
	hosts = []string{"192.168.1.101"}
	_, err = buildHostCatalogPathMap(hosts, vdb)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "host 192.168.1.101 has empty name")
}

func TestCalculateLastGoodEpoch(t *testing.T) {
	vcc := VClusterCommands{}
	logger := vlog.Printer{}

	// Test with empty epoch info list
	epochInfoList := []EpochInfo{}
	_, err := vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "no epoch info provided")

	// Test with valid epoch info - majority consensus
	epochInfoList = []EpochInfo{
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node1"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node2"},
		{Epoch: "99", Timestamp: "2025-10-20 11:59:00", KSafety: "1", Hostname: "node3"},
	}

	epoch, err := vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), epoch)

	// Test with 5 nodes, 3 with same epoch
	epochInfoList = []EpochInfo{
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node1"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node2"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node3"},
		{Epoch: "99", Timestamp: "2025-10-20 11:59:00", KSafety: "1", Hostname: "node4"},
		{Epoch: "98", Timestamp: "2025-10-20 11:58:00", KSafety: "1", Hostname: "node5"},
	}
	epoch, err = vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), epoch)

	// Test with inconsistent k-safety
	epochInfoList = []EpochInfo{
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node1"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "2", Hostname: "node2"},
	}

	_, err = vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "inconsistent ksafety")

	// Test with invalid epoch values
	epochInfoList = []EpochInfo{
		{Epoch: "invalid", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node1"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node2"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node3"},
	}

	epoch, err = vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), epoch)

	// Test with invalid k-safety values
	epochInfoList = []EpochInfo{
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "invalid", Hostname: "node1"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node2"},
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node3"},
	}

	epoch, err = vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.NoError(t, err)
	assert.Equal(t, int64(100), epoch)

	// Test failure to find majority consensus
	epochInfoList = []EpochInfo{
		{Epoch: "100", Timestamp: "2025-10-20 12:00:00", KSafety: "1", Hostname: "node1"},
		{Epoch: "99", Timestamp: "2025-10-20 11:59:00", KSafety: "1", Hostname: "node2"},
		{Epoch: "98", Timestamp: "2025-10-20 11:58:00", KSafety: "1", Hostname: "node3"},
	}

	_, err = vcc.calculateLastGoodEpoch(epochInfoList, logger)
	assert.Error(t, err)
	assert.ErrorContains(t, err, "failed to find majority of nodes")
}
