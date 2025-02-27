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
	"os"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

// Date format for workload capture/replay start/end timestamps
const workloadDateFormat = "2006-01-02 15:04:05.999999-07"

type workloadQuery struct {
	NodeName          string `json:"node_name" csv:"node_name"`
	SessionID         string `json:"session_id" csv:"session_id"`
	StartTimestamp    string `json:"start_timestamp" csv:"start_timestamp"`
	EndTimestamp      string `json:"end_timestamp" csv:"end_timestamp"`
	Request           string `json:"request" csv:"request"`
	RequestDurationMS int64  `json:"request_duration_ms" csv:"request_duration_ms"`
	ErrorDetails      string `json:"error_details" csv:"error_details"`
}

type workloadCaptureOptions struct {
	WorkloadFileLocation string
	StartTimestamp       string
	EndTimestamp         string
}

type VWorkloadCaptureOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: workload capture options
	workloadCaptureOptions
}

func VWorkloadCaptureOptionsFactory() VWorkloadCaptureOptions {
	options := VWorkloadCaptureOptions{}
	options.setDefaultValues()
	return options
}

// Validate workload capture start/end timestamps are formatted correctly and start time comes before end time
func (options *VWorkloadCaptureOptions) validateCaptureTimestamps() error {
	if options.StartTimestamp == "" {
		return fmt.Errorf("must provide start timestamp")
	}

	startTime, err := time.Parse(workloadDateFormat, options.StartTimestamp)
	if err != nil {
		return fmt.Errorf("failed to parse start timestamp, %w", err)
	}

	if options.EndTimestamp == "" {
		return fmt.Errorf("must provide end timestamp")
	}

	endTime, err := time.Parse(workloadDateFormat, options.EndTimestamp)
	if err != nil {
		return fmt.Errorf("failed to parse end timestamp, %w", err)
	}

	if startTime.After(endTime) || startTime.Equal(endTime) {
		return fmt.Errorf("start time must be before end time")
	}

	return nil
}

// Validate workload file location does not exist and can be written to
func (options *VWorkloadCaptureOptions) validateWorkloadFileLocation() error {
	if options.WorkloadFileLocation == "" {
		return fmt.Errorf("must provide workload file location")
	}

	pathAccess := util.CanWriteAccessPath(options.WorkloadFileLocation)
	if pathAccess == util.NoWritePerm {
		return fmt.Errorf("no permission to write to workload file location")
	}
	if pathAccess == util.FileExist {
		return fmt.Errorf("file already exists at workload file location")
	}

	return nil
}

// Validate all options required for workload capture
func (options *VWorkloadCaptureOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(WorkloadCaptureCmd, logger)
	if err != nil {
		return err
	}

	err = options.validateWorkloadFileLocation()
	if err != nil {
		return err
	}

	err = options.validateCaptureTimestamps()
	if err != nil {
		return err
	}
	return nil
}

func (options *VWorkloadCaptureOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VWorkloadCaptureOptions) analyzeOptions() (err error) {
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

func (options *VWorkloadCaptureOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// Save workload capture results as a CSV file
func saveWorkloadCaptureCSV(capturedWorkload []workloadQuery, workloadFileLocation string) error {
	csvData, err := util.ConvertToCSVRows(capturedWorkload)
	if err != nil {
		return err
	}

	const csvFilePermissions = os.FileMode(0644)
	err = util.WriteCSV(workloadFileLocation, csvData, csvFilePermissions)
	if err != nil {
		return err
	}

	return nil
}

// VWorkloadCapture captures a workload and saves it as a CSV file
func (vcc VClusterCommands) VWorkloadCapture(options *VWorkloadCaptureOptions) error {
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

	// produce workload capture instructions
	capturedWorkload := []workloadQuery{}
	instructions, err := vcc.produceWorkloadCaptureInstructions(options, &capturedWorkload)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add certs to the engine
	clusterOpEngine := makeClusterOpEngine(instructions, options)

	// give the instructions to the VClusterOpEngine to run
	runError := clusterOpEngine.run(vcc.Log)
	if runError != nil {
		return fmt.Errorf("fail to capture workload: %w", runError)
	}

	// Save captured workload as a CSV file
	err = saveWorkloadCaptureCSV(capturedWorkload, options.WorkloadFileLocation)
	if err != nil {
		return fmt.Errorf("fail to save workload capture CSV: %w", err)
	}

	return nil
}

// The generated instructions will later perform the following operations necessary for workload capture
//   - Check NMA connectivity
//   - Capture workload and save as CSV file
func (vcc VClusterCommands) produceWorkloadCaptureInstructions(options *VWorkloadCaptureOptions,
	capturedWorkload *[]workloadQuery) ([]clusterOp, error) {
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
		return instructions, fmt.Errorf("found no UP nodes for capture workload")
	}

	// Get initiator host to send NMA requests to
	initiator, err := getInitiatorHost(vdb.PrimaryUpNodes, []string{} /* skip hosts */)
	if err != nil {
		return instructions, err
	}
	initiatorHost := []string{initiator}

	nmaHealthOp := makeNMAHealthOp(initiatorHost)

	nmaWorkloadCaptureData := nmaWorkloadCaptureRequestData{}
	nmaWorkloadCaptureData.DBName = options.DBName
	nmaWorkloadCaptureData.UserName = options.UserName
	nmaWorkloadCaptureData.Password = options.Password
	nmaWorkloadCaptureData.StartTimestamp = options.StartTimestamp
	nmaWorkloadCaptureData.EndTimestamp = options.EndTimestamp

	nmaWorkloadCaptureOp, err := makeNMAWorkloadCaptureOp(initiatorHost, options.usePassword,
		&nmaWorkloadCaptureData, options.WorkloadFileLocation, capturedWorkload)
	if err != nil {
		return instructions, err
	}

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaWorkloadCaptureOp,
	)

	return instructions, nil
}
