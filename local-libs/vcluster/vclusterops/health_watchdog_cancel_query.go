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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// healthWatchdogCancelQueryOptions represents a single session or statement to be canceled.
type HealthWatchdogCancelQueryOptions struct {
	SessionID   string `json:"session_id"`
	StatementID int64  `json:"statement_id,omitempty"`
}

// HealthWatchdogCancelQueryResponse represents a single session or statement canceled response.
type HealthWatchdogCancelQueryResponse struct {
	Status      string `json:"status"`
	Message     string `json:"message"`
	SessionID   string `json:"session_id"`
	StatementID int64  `json:"statement_id,omitempty"`
}

type VHealthWatchdogCancelQueryOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: health watchdog cancel-query options
	Sessions []HealthWatchdogCancelQueryOptions
}

func VHealthWatchdogCancelQueryOptionsFactory() VHealthWatchdogCancelQueryOptions {
	options := VHealthWatchdogCancelQueryOptions{}
	options.setDefaultValues()
	return options
}

// Validate all options required for health watchdog cancel-query endpoint
func (options *VHealthWatchdogCancelQueryOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(HealthWatchdogCancelQueryCmd, logger)
	if err != nil {
		return err
	}

	// Check for an empty list.
	if len(options.Sessions) == 0 {
		return fmt.Errorf("no sessions provided for cancellation")
	}

	// validate SessionID
	for _, session := range options.Sessions {
		if session.SessionID == "" {
			return fmt.Errorf("session_id is required to cancel a query")
		}
	}

	return nil
}

func (options *VHealthWatchdogCancelQueryOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VHealthWatchdogCancelQueryOptions) analyzeOptions() (err error) {
	// we analyze host names when it is set in user input, otherwise we use hosts in yaml config
	if len(options.RawHosts) > 0 {
		// resolve RawHosts to be IP addresses
		hostAddresses, err := util.ResolveRawHostsToAddresses(options.RawHosts, options.IPv6)
		if err != nil {
			return err
		}
		options.Hosts = hostAddresses
	}
	return nil
}

func (options *VHealthWatchdogCancelQueryOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VHealthWatchdogCancelQuery cancels health watchdog query
func (vcc VClusterCommands) VHealthWatchdogCancelQuery(options *VHealthWatchdogCancelQueryOptions) (
	[]HealthWatchdogCancelQueryResponse, error) {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	var result []HealthWatchdogCancelQueryResponse

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return nil, err
	}

	// produce health watchdog cancel-query instructions
	instructions, err := vcc.produceHealthWatchdogCancelQueryInstructions(options, &result)
	if err != nil {
		return nil, fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add latest context to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return nil, fmt.Errorf("fail to cancel health watchdog query: %w", runError)
	}
	return result, runError
}

// The generated instructions will later perform the following operations necessary for health watchdog cancel-query
//   - Check NMA connectivity
//   - Make request to NMA to cancel health watchdog query
func (vcc VClusterCommands) produceHealthWatchdogCancelQueryInstructions(options *VHealthWatchdogCancelQueryOptions,
	healthWatchdogCancelQueryResp *[]HealthWatchdogCancelQueryResponse) ([]clusterOp, error) {
	var instructions []clusterOp
	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDB(&vdb, &options.DatabaseOptions)
	if err != nil {
		return instructions, err
	}

	// Get up hosts
	hosts := options.Hosts
	hosts = vdb.filterUpHostListBySandbox(hosts, util.MainClusterSandbox)
	if len(hosts) == 0 {
		return instructions, fmt.Errorf("found no UP nodes for health watchdog cancel-query")
	}

	// Get initiator host to send NMA requests to
	initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if err != nil {
		return instructions, err
	}
	initiatorHost := []string{initiator}

	nmaHealthOp := makeNMAHealthOp(initiatorHost)

	nmaHealthWatchdogCancelQueryData := nmaHealthWatchdogCancelQueryData{}
	nmaHealthWatchdogCancelQueryData.DBName = options.DBName
	nmaHealthWatchdogCancelQueryData.UserName = options.UserName
	nmaHealthWatchdogCancelQueryData.Password = options.Password

	var cancellationQueries []HealthWatchdogCancelQueryOptions
	for _, task := range options.Sessions {
		cancellationQueries = append(cancellationQueries, HealthWatchdogCancelQueryOptions{
			SessionID:   task.SessionID,
			StatementID: task.StatementID,
		})
	}
	nmaHealthWatchdogCancelQueryData.Sessions = cancellationQueries

	nmaHealthWatchdogCancelQueryOp, err := makeHealthWatchdogCancelQueryOp(hosts, options.usePassword,
		&nmaHealthWatchdogCancelQueryData, healthWatchdogCancelQueryResp)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaHealthWatchdogCancelQueryOp,
	)

	return instructions, nil
}
