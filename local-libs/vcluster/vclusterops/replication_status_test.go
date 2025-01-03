/*
 (c) Copyright [2023-2024] Open Text.
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
)

const (
	node1              = "node1"
	node2              = "node2"
	loadSnapshotPrepOp = "load_snapshot_prep"
	dataTransferOp     = "data_transfer"
	loadSnapshotOp     = "load_snapshot"
	startedStatus      = "started"
	failedStatus       = "failed"
	completedStatus    = "completed"
	transactionID      = 12345678901234567
)

// Setup - status objects for testing
var (
	node1LoadSnapshotPrepStarted = ReplicationStatusResponse{
		OpName:        loadSnapshotPrepOp,
		Status:        startedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "",
		SentBytes:     0,
		TotalBytes:    0,
		TransactionID: transactionID,
	}
	node1LoadSnapshotPrep = ReplicationStatusResponse{
		OpName:        loadSnapshotPrepOp,
		Status:        completedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:12 EDT 2024",
		SentBytes:     0,
		TotalBytes:    0,
		TransactionID: transactionID,
	}
	node1DataTransfer = ReplicationStatusResponse{
		OpName:        dataTransferOp,
		Status:        completedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     1024,
		TotalBytes:    1024,
		TransactionID: transactionID,
	}
	node1LoadSnapshotFailed = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        failedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     1024,
		TotalBytes:    1024,
		TransactionID: transactionID,
	}
	node1LoadSnapshotCompleted = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        completedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     1024,
		TotalBytes:    1024,
		TransactionID: transactionID,
	}

	node2LoadSnapshotPrep = ReplicationStatusResponse{
		OpName:        loadSnapshotPrepOp,
		Status:        completedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:12 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     0,
		TotalBytes:    0,
		TransactionID: transactionID,
	}
	node2DataTransferStarted = ReplicationStatusResponse{
		OpName:        dataTransferOp,
		Status:        startedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:12 EDT 2024",
		EndTime:       "",
		SentBytes:     128,
		TotalBytes:    2048,
		TransactionID: transactionID,
	}
	node2DataTransfer = ReplicationStatusResponse{
		OpName:        dataTransferOp,
		Status:        completedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:12 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:14 EDT 2024",
		SentBytes:     2048,
		TotalBytes:    2048,
		TransactionID: transactionID,
	}
	node2LoadSnapshotCompleted = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        completedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:12 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:14 EDT 2024",
		SentBytes:     2048,
		TotalBytes:    2048,
		TransactionID: transactionID,
	}
)

func TestGetFinalReplicationStatus(t *testing.T) {
	// Negative - 1 node target DB, empty status list
	replicationStatus := []ReplicationStatusResponse{}
	actualStatus := getFinalReplicationStatus(replicationStatus)
	assert.Nil(t, actualStatus)

	// Positive - 1 node target DB, "load_snapshot_prep" in progress
	replicationStatus = []ReplicationStatusResponse{node1LoadSnapshotPrepStarted}

	expectedStatus := ReplicationStatusResponse{
		OpName:        loadSnapshotPrepOp,
		Status:        startedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "",
		SentBytes:     0,
		TotalBytes:    0,
		TransactionID: transactionID,
	}

	actualStatus = getFinalReplicationStatus(replicationStatus)
	assert.Equal(t, expectedStatus, *actualStatus)

	// Positive - 1 node target DB, "load_snapshot" op failed
	replicationStatus = []ReplicationStatusResponse{
		node1LoadSnapshotPrep, node1DataTransfer, node1LoadSnapshotFailed}

	expectedStatus = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        failedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     1024,
		TotalBytes:    1024,
		TransactionID: transactionID,
	}

	actualStatus = getFinalReplicationStatus(replicationStatus)
	assert.Equal(t, expectedStatus, *actualStatus)

	// Positive - 1 node target DB, all ops complete
	replicationStatus = []ReplicationStatusResponse{
		node1LoadSnapshotPrep, node1DataTransfer, node1LoadSnapshotCompleted}

	expectedStatus = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        completedStatus,
		NodeName:      node1,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:13 EDT 2024",
		SentBytes:     1024,
		TotalBytes:    1024,
		TransactionID: transactionID,
	}

	actualStatus = getFinalReplicationStatus(replicationStatus)
	assert.Equal(t, expectedStatus, *actualStatus)

	// Positive - 2 node target DB, node 2 data transfer still in progress
	replicationStatus = []ReplicationStatusResponse{
		node1LoadSnapshotPrep, node1DataTransfer, node1LoadSnapshotCompleted,
		node2LoadSnapshotPrep, node2DataTransferStarted}

	expectedStatus = ReplicationStatusResponse{
		OpName:        dataTransferOp,
		Status:        startedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "",
		SentBytes:     128,
		TotalBytes:    2048,
		TransactionID: transactionID,
	}

	actualStatus = getFinalReplicationStatus(replicationStatus)
	assert.Equal(t, expectedStatus, *actualStatus)

	// Positive - 2 node target DB, all ops completed on all nodes
	replicationStatus = []ReplicationStatusResponse{
		node1LoadSnapshotPrep, node1DataTransfer, node1LoadSnapshotCompleted,
		node2LoadSnapshotPrep, node2DataTransfer, node2LoadSnapshotCompleted}

	expectedStatus = ReplicationStatusResponse{
		OpName:        loadSnapshotOp,
		Status:        completedStatus,
		NodeName:      node2,
		StartTime:     "Mon Sep 23 16:08:11 EDT 2024",
		EndTime:       "Mon Sep 23 16:08:14 EDT 2024",
		SentBytes:     2048,
		TotalBytes:    2048,
		TransactionID: transactionID,
	}

	actualStatus = getFinalReplicationStatus(replicationStatus)
	assert.Equal(t, expectedStatus, *actualStatus)
}
