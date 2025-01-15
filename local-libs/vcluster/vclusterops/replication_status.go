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
	"fmt"
	"sort"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type VReplicationStatusDatabaseOptions struct {
	TargetDB      DatabaseOptions
	TransactionID int64
}

type ReplicationStatusResponse struct {
	// Time replication was started
	StartTime string `json:"start_time"`

	// End time, if replication has completed
	EndTime string `json:"end_time"`

	// Current replication operation name. Possible values in order:
	// - 'load_snapshot_prep'
	// - 'data_transfer' - optional if source and target communal storage
	//    are the same
	// - 'load_snapshot' - replication is complete if this op has a
	//    status of 'completed'
	OpName string `json:"op_name"`

	// Current replication operation status. Possible values:
	//   'started', 'failed', 'completed'
	Status string `json:"status"`

	// Node the current replication operation is on
	NodeName string `json:"node_name"`

	// Number of bytes transferred as part of replication
	SentBytes int64 `json:"sent_bytes"`

	// Total number of bytes to be transferred as part of replication
	TotalBytes    int64 `json:"total_bytes"`
	TransactionID int64 `json:"txn_id"`
}

func VReplicationStatusFactory() VReplicationStatusDatabaseOptions {
	options := VReplicationStatusDatabaseOptions{}
	return options
}

func (options *VReplicationStatusDatabaseOptions) validateRequiredOptions(_ vlog.Printer) error {
	if len(options.TargetDB.Hosts) == 0 {
		return fmt.Errorf("must specify a target host or target host list")
	}

	// validate target database
	if options.TargetDB.DBName == "" {
		return fmt.Errorf("must specify a target database name")
	}
	err := util.ValidateDBName(options.TargetDB.DBName)
	if err != nil {
		return err
	}

	// need to provide a password or TLSconfig if source and target username are different
	if options.TargetDB.Password == nil {
		return fmt.Errorf("must specify a target password")
	}

	if options.TransactionID <= 0 {
		return fmt.Errorf("must specify a valid transaction ID")
	}

	return nil
}

func (options *VReplicationStatusDatabaseOptions) validateParseOptions(logger vlog.Printer) error {
	// batch 1: validate required params
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VReplicationStatusDatabaseOptions) analyzeOptions() (err error) {
	if len(options.TargetDB.Hosts) > 0 {
		// resolve RawHosts to be IP addresses
		options.TargetDB.Hosts, err = util.ResolveRawHostsToAddresses(options.TargetDB.Hosts, options.TargetDB.IPv6)
		if err != nil {
			return err
		}
	}
	return nil
}

func (options *VReplicationStatusDatabaseOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VReplicationStatus returns the status of an asynchronous replication job based on a transaction ID
func (vcc VClusterCommands) VReplicationStatus(options *VReplicationStatusDatabaseOptions) (*ReplicationStatusResponse, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// produce database replication status instructions
	replicationStatus := []ReplicationStatusResponse{}
	instructions, err := vcc.produceReplicationStatusInstructions(options, &replicationStatus)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, &options.TargetDB)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to get replication status: %w", runError)
	}

	if len(replicationStatus) == 0 {
		return nil, fmt.Errorf("invalid transaction ID")
	}
	finalReplicationStatus := getFinalReplicationStatus(replicationStatus)
	return finalReplicationStatus, nil
}

// The generated instructions will later perform the following operations necessary
// for a successful replication status retrieval
//   - Check NMA connectivity
//   - Get replication status
func (vcc VClusterCommands) produceReplicationStatusInstructions(options *VReplicationStatusDatabaseOptions,
	replicationStatus *[]ReplicationStatusResponse) ([]clusterOp, error) {
	var instructions []clusterOp

	// verify the username for connecting to the target database
	targetUsePassword := false
	if options.TargetDB.Password != nil {
		targetUsePassword = true
		if options.TargetDB.UserName == "" {
			username, e := util.GetCurrentUsername()
			if e != nil {
				return instructions, e
			}
			options.TargetDB.UserName = username
		}
		vcc.Log.Info("Current target username", "username", options.TargetDB.UserName)
	}

	nmaHealthOp := makeNMAHealthOp(options.TargetDB.Hosts)

	nmaReplicationStatusData := nmaReplicationStatusRequestData{}
	nmaReplicationStatusData.DBName = options.TargetDB.DBName
	nmaReplicationStatusData.ExcludedTransactionIDs = []int64{} // Doesn't matter since we specify a transaction ID
	nmaReplicationStatusData.GetTransactionIDsOnly = false      // Get all replication status info
	nmaReplicationStatusData.TransactionID = options.TransactionID
	nmaReplicationStatusData.UserName = options.TargetDB.UserName
	nmaReplicationStatusData.Password = options.TargetDB.Password

	nmaReplicationStatusOp, err := makeNMAReplicationStatusOp(options.TargetDB.Hosts, targetUsePassword,
		&nmaReplicationStatusData, nil, replicationStatus)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaReplicationStatusOp,
	)

	return instructions, nil
}

func getFinalReplicationStatus(replicationStatus []ReplicationStatusResponse) *ReplicationStatusResponse {
	if len(replicationStatus) == 0 {
		return nil
	}

	// Used to determine replication op order - replication ops with higher int values happen later
	opOrder := make(map[string]int)
	opOrder["load_snapshot_prep"] = 0
	opOrder["data_transfer"] = 1
	opOrder["load_snapshot"] = 2

	// Sort statuses by start time, node name, then op name. This lets us search chronologically through the statuses
	// to find the first failure or in-progress op if there is one
	sort.Slice(replicationStatus, func(i, j int) bool {
		iStatus := replicationStatus[i]
		jStatus := replicationStatus[j]

		if iStatus.StartTime != jStatus.StartTime {
			iStart, _ := time.Parse(time.UnixDate, iStatus.StartTime)
			jStart, _ := time.Parse(time.UnixDate, jStatus.StartTime)
			return iStart.Before(jStart)
		} else if iStatus.NodeName != jStatus.NodeName {
			return iStatus.NodeName < jStatus.NodeName
		}
		return opOrder[iStatus.OpName] < opOrder[jStatus.OpName]
	})

	// Get basic status info from the first op that was started
	firstOp := replicationStatus[0]
	finalReplicationStatus := ReplicationStatusResponse{}
	finalReplicationStatus.TransactionID = firstOp.TransactionID
	finalReplicationStatus.StartTime = firstOp.StartTime

	// Get the rest of the status info from the current op (the last op in the sorted list)
	currentOp := replicationStatus[len(replicationStatus)-1]

	finalReplicationStatus.Status = currentOp.Status
	finalReplicationStatus.EndTime = currentOp.EndTime
	finalReplicationStatus.OpName = currentOp.OpName
	finalReplicationStatus.SentBytes = currentOp.SentBytes
	finalReplicationStatus.TotalBytes = currentOp.TotalBytes
	finalReplicationStatus.NodeName = currentOp.NodeName

	return &finalReplicationStatus
}
