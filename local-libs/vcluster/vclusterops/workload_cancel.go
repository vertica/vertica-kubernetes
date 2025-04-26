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

type workloadCancelResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

type workloadCancelOptions struct {
	JobID   int64
	Sandbox string
}

type VWorkloadCancelOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: workload cancel options
	workloadCancelOptions
}

func VWorkloadCancelOptionsFactory() VWorkloadCancelOptions {
	options := VWorkloadCancelOptions{}
	options.setDefaultValues()
	return options
}

// Validate all options required for workload cancel
func (options *VWorkloadCancelOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(WorkloadCancelCmd, logger)
	if err != nil {
		return err
	}
	return nil
}

func (options *VWorkloadCancelOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VWorkloadCancelOptions) analyzeOptions() (err error) {
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

func (options *VWorkloadCancelOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// VWorkloadCancel cancels a workload and saves it as a CSV file
func (vcc VClusterCommands) VWorkloadCancel(options *VWorkloadCancelOptions) error {
	/*
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 */

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// produce workload cancel instructions
	instructions, err := vcc.produceWorkloadCancelInstructions(options)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to cancel workload: %w", runError)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary for workload cancel
//   - Check NMA connectivity
//   - Cancel workload and save as CSV file
func (vcc VClusterCommands) produceWorkloadCancelInstructions(options *VWorkloadCancelOptions) ([]clusterOp, error) {
	var instructions []clusterOp

	vdb := makeVCoordinationDatabase()

	err := vcc.getVDBFromRunningDBIncludeSandbox(&vdb, &options.DatabaseOptions, options.Sandbox)
	if err != nil {
		return instructions, err
	}

	// Get up hosts in the specified sandbox
	hosts := options.Hosts
	hosts = vdb.filterUpHostListBySandbox(hosts, options.Sandbox)
	if len(hosts) == 0 {
		return instructions, fmt.Errorf("found no UP nodes for workload replay")
	}

	// Get initiator host to send NMA requests to
	initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if err != nil {
		return instructions, err
	}
	initiatorHost := []string{initiator}

	nmaHealthOp := makeNMAHealthOp(initiatorHost)

	nmaWorkloadCancelData := nmaWorkloadCancelRequestData{}
	nmaWorkloadCancelData.DBName = options.DBName
	nmaWorkloadCancelData.UserName = options.UserName
	nmaWorkloadCancelData.Password = options.Password
	nmaWorkloadCancelData.JobID = options.JobID

	nmaWorkloadReplayOp, err := makeNMAWorkloadCancelOp(initiatorHost, options.usePassword, &nmaWorkloadCancelData)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaWorkloadReplayOp,
	)

	return instructions, nil
}
