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
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type workloadReplayOptions struct {
	WorkloadFileLocation      string
	ReplayResultsFileLocation string
	Sandbox                   string
	JobID                     int64
	QuickReplay               bool
}

type VWorkloadReplayOptions struct {
	// Part 1: basic db info
	DatabaseOptions

	// Part 2: workload replay options
	workloadReplayOptions
}

func VWorkloadReplayOptionsFactory() VWorkloadReplayOptions {
	options := VWorkloadReplayOptions{}
	options.setDefaultValues()
	return options
}

// Validate workload file exists and can be read from
func (options *VWorkloadReplayOptions) validateWorkloadFileLocation() error {
	if options.WorkloadFileLocation == "" {
		return fmt.Errorf("must provide workload file location")
	}

	if !strings.HasSuffix(options.WorkloadFileLocation, ".csv") {
		return fmt.Errorf("must provide .csv workload file")
	}

	err := util.AbsPathCheck(options.WorkloadFileLocation)
	if err != nil {
		return fmt.Errorf("must provide an absolute path for workload file location")
	}

	pathAccess := util.CanReadAccessPath(options.WorkloadFileLocation)
	if pathAccess == util.NoReadPerm {
		return fmt.Errorf("no permission to read from workload file location")
	}
	if pathAccess == util.FileNotExist {
		return fmt.Errorf("workload file location does not exist")
	}

	return nil
}

// Validate replay results file doesn't exist and can be written to
func (options *VWorkloadReplayOptions) validateReplayResultsFileLocation() error {
	if options.ReplayResultsFileLocation == "" {
		return fmt.Errorf("must provide replay results file location")
	}

	err := util.AbsPathCheck(options.ReplayResultsFileLocation)
	if err != nil {
		return fmt.Errorf("must provide an absolute path for replay results file location")
	}

	pathAccess := util.CanWriteAccessPath(options.ReplayResultsFileLocation)
	if pathAccess == util.NoWritePerm {
		return fmt.Errorf("no permission to write to replay results file location")
	}
	if pathAccess == util.FileExist {
		return fmt.Errorf("file already exists at replay results file location")
	}

	return nil
}

// Validate sandbox name
func (options *VWorkloadReplayOptions) validateSandbox() error {
	if options.Sandbox == "" {
		return fmt.Errorf("must provide sandbox")
	}

	err := util.ValidateSandboxName(options.Sandbox)
	if err != nil {
		return err
	}

	return nil
}

// Validate all options required for workload replay
func (options *VWorkloadReplayOptions) validateRequiredOptions(logger vlog.Printer) error {
	// Validate DB name, hosts, etc.
	err := options.validateBaseOptions(WorkloadReplayCmd, logger)
	if err != nil {
		return err
	}

	err = options.validateWorkloadFileLocation()
	if err != nil {
		return err
	}

	err = options.validateReplayResultsFileLocation()
	if err != nil {
		return err
	}

	err = options.validateSandbox()
	if err != nil {
		return err
	}

	if options.JobID == 0 {
		return fmt.Errorf("JobID must be provided")
	}
	return nil
}

func (options *VWorkloadReplayOptions) validateParseOptions(logger vlog.Printer) error {
	err := options.validateRequiredOptions(logger)
	if err != nil {
		return err
	}

	return nil
}

// analyzeOptions will modify some options based on what is chosen
func (options *VWorkloadReplayOptions) analyzeOptions() (err error) {
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

func (options *VWorkloadReplayOptions) validateAnalyzeOptions(logger vlog.Printer) error {
	if err := options.validateParseOptions(logger); err != nil {
		return err
	}
	return options.analyzeOptions()
}

// Load workload data from a CSV-formatted workload file
func loadWorkloadCSV(workloadFileLocation string) ([]workloadQuery, error) {
	capturedWorkload := []workloadQuery{}

	csvData, err := util.ReadCSV(workloadFileLocation)
	if err != nil {
		return capturedWorkload, err
	}

	capturedWorkload, err = convertFromCSV(csvData)
	if err != nil {
		return capturedWorkload, err
	}

	return capturedWorkload, nil
}

// Convert from 2D slice of strings to slice of captured workload objects
func convertFromCSV(data [][]string) ([]workloadQuery, error) {
	capturedWorkloadRequests := []workloadQuery{}

	// Iterate over all rows except the first header row
	for _, row := range data[1:] {
		request := workloadQuery{}

		for colIndex, col := range row {
			// Populate fields based on header name
			headerName := data[0][colIndex]
			switch headerName {
			case "node_name":
				request.NodeName = col
			case "session_id":
				request.SessionID = col
			case "start_timestamp":
				request.StartTimestamp = col
			case "end_timestamp":
				request.EndTimestamp = col
			case "request":
				request.Request = col
			case "request_duration_ms":
				requestDuration, err := strconv.ParseInt(col, 10, 64)
				if err != nil {
					return nil, fmt.Errorf("fail to parse CSV value request_duration_ms '%s': %w", col, err)
				}
				request.RequestDurationMS = requestDuration
			case "error_details":
				request.ErrorDetails = col
			default:
				return nil, fmt.Errorf("fail to parse CSV: unknown header '%s'", headerName)
			}
		}
		capturedWorkloadRequests = append(capturedWorkloadRequests, request)
	}

	return capturedWorkloadRequests, nil
}

type WorkloadReplayReportData struct {
	Request            string `json:"request" csv:"request"`
	OriginalDurationMS int64  `json:"original_duration_ms" csv:"original_duration_ms"`
	OriginalNodeName   string `json:"original_node_name" csv:"original_node_name"`
	ReplayDurationMS   int64  `json:"replay_duration_ms" csv:"replay_duration_ms"`
	ReplayNodeName     string `json:"replay_node_name" csv:"replay_node_name"`
	ErrorDetails       string `json:"error" csv:"error"`
}

// Aggregate original captured workload and replay information into one struct that can be written to a CSV file
func aggregateWorkloadReplayReportData(data workloadReplayData) []WorkloadReplayReportData {
	reportData := []WorkloadReplayReportData{}

	for index, originalRow := range data.originalWorkloadData {
		var replayDurationMS int64
		var replayNodeName string
		var errorDetails string

		if index < len(data.replayData) {
			replayRow := data.replayData[index]
			replayDurationMS = replayRow.RequestDurationMS
			replayNodeName = replayRow.NodeName
			errorDetails = replayRow.ErrorDetails
		} else {
			replayDurationMS = 0
			replayNodeName = ""
			errorDetails = "Workload replay was Canceled"
		}

		reportData = append(reportData, WorkloadReplayReportData{
			Request:            originalRow.Request,
			OriginalDurationMS: originalRow.RequestDurationMS,
			OriginalNodeName:   originalRow.NodeName,
			ReplayDurationMS:   replayDurationMS,
			ReplayNodeName:     replayNodeName,
			ErrorDetails:       errorDetails,
		})
	}
	return reportData
}

// Save replay results as a CSV file
func saveWorkloadReplayReportCSV(data workloadReplayData, replayResultsFileLocation string) error {
	reportData := aggregateWorkloadReplayReportData(data)

	csvData, err := util.ConvertToCSVRows(reportData)
	if err != nil {
		return fmt.Errorf("fail to format CSV rows, %w", err)
	}

	const csvFilePermissions = os.FileMode(0644)
	err = util.WriteCSV(replayResultsFileLocation, csvData, csvFilePermissions)
	if err != nil {
		return err
	}

	return nil
}

// VWorkloadReplay replays a workload and saves a comparison of the original vs. replay timings in a CSV file
func (vcc VClusterCommands) VWorkloadReplay(ctx context.Context, options *VWorkloadReplayOptions) error {
	/*
	 *   - Read/parse the captured workload CSV file
	 *   - Produce Instructions
	 *   - Create a VClusterOpEngine
	 *   - Give the instructions to the VClusterOpEngine to run
	 *   - Save workload replay results to a CSV file
	 */

	// validate and analyze options
	err := options.validateAnalyzeOptions(vcc.Log)
	if err != nil {
		return err
	}

	// Read/parse the captured workload CSV file
	originalWorkload, err := loadWorkloadCSV(options.WorkloadFileLocation)
	if err != nil {
		return fmt.Errorf("fail to load workload capture file, %w", err)
	}

	replayData := workloadReplayData{
		originalWorkloadData: originalWorkload,
		replayData:           []workloadQuery{},
	}

	// produce workload replay instructions
	instructions, err := vcc.produceWorkloadReplayInstructions(options, &replayData)
	if err != nil {
		return fmt.Errorf("fail to produce instructions, %w", err)
	}

	// create a VClusterOpEngine, and add latest context to the engine
	execContext := &opEngineExecContext{workloadReplyCtx: ctx}

	clusterOpEngine := makeClusterOpEngine(instructions, options)
	clusterOpEngine.execContext = execContext

	// wait for workload execution to finish before proceeding with next steps
	var wg sync.WaitGroup
	wg.Add(1)
	var runError error
	go func() {
		defer wg.Done()
		runError = clusterOpEngine.runInSandboxWithExistingCtx(vcc.Log, nil /*vdb*/, util.MainClusterSandbox)
	}()

	vcc.Log.Info("VWorkloadReplay: Waiting for workload execution to finish.")
	wg.Wait()

	err = saveWorkloadReplayReportCSV(replayData, options.ReplayResultsFileLocation)
	if err != nil {
		vcc.Log.PrintInfo("fail to save workload replay report CSV: %v", err)
		return fmt.Errorf("fail to save workload replay report CSV: %v", err)
	}

	if ctx.Err() != nil {
		vcc.Log.PrintInfo("VWorkloadReplay: Context canceled.")
		return ctx.Err()
	}
	return runError
}

// The generated instructions will later perform the following operations necessary for workload replay
//   - Check NMA connectivity
//   - Replay workload
func (vcc VClusterCommands) produceWorkloadReplayInstructions(options *VWorkloadReplayOptions,
	workloadReplayData *workloadReplayData) (
	[]clusterOp, error) {
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

	nmaWorkloadReplayData := nmaWorkloadReplayRequestData{}
	nmaWorkloadReplayData.DBName = options.DBName
	nmaWorkloadReplayData.UserName = options.UserName
	nmaWorkloadReplayData.Password = options.Password
	nmaWorkloadReplayData.JobID = options.JobID

	nmaWorkloadReplayOp, err := makeNMAWorkloadReplayOp(initiatorHost, options.usePassword, vdb.HostNodeMap,
		&nmaWorkloadReplayData, workloadReplayData)
	if err != nil {
		return instructions, err
	}
	nmaWorkloadReplayOp.quickReplay = options.QuickReplay

	instructions = append(instructions,
		&nmaHealthOp,
		&nmaWorkloadReplayOp,
	)

	return instructions, nil
}
