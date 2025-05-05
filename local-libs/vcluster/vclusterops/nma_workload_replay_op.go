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
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/vertica/vcluster/vclusterops/util"
)

type workloadReplayData struct {
	originalWorkloadData []workloadQuery // Data from original workload capture
	replayData           []workloadQuery // Data captured during replay
}

type nmaWorkloadReplayOp struct {
	opBase
	nmaWorkloadReplayRequestData
	hosts              []string
	hostNodeMap        vHostNodeMap
	hostRequestBodyMap map[string]string

	workloadReplayData *workloadReplayData
	JobID              int64
}

func makeNMAWorkloadReplayOp(hosts []string, usePassword bool, hostNodeMap vHostNodeMap,
	workloadReplayRequestData *nmaWorkloadReplayRequestData,
	workloadReplayData *workloadReplayData) (nmaWorkloadReplayOp, error) {
	op := nmaWorkloadReplayOp{}
	op.name = "NMAWorkloadReplayOp"
	op.description = "Replay workload"
	op.hosts = hosts
	op.hostNodeMap = hostNodeMap
	op.nmaWorkloadReplayRequestData = *workloadReplayRequestData
	op.workloadReplayData = workloadReplayData
	op.JobID = workloadReplayRequestData.JobID

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, workloadReplayRequestData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = workloadReplayRequestData.UserName
		op.Password = workloadReplayRequestData.Password
	}

	return op, nil
}

// Request data to be sent to NMA capture workload endpoint
type nmaWorkloadReplayRequestData struct {
	DBName   string  `json:"dbname"`
	UserName string  `json:"username"`
	Password *string `json:"password"`
	Request  string  `json:"request"`
	StmtType string  `json:"statement_type"`
	FileName string  `json:"file_name"`
	FileDir  string  `json:"file_dir"`
	JobID    int64   `json:"job_id"`
}

// Create the request body JSON string for a preprocessed workload query
func (op *nmaWorkloadReplayOp) updateRequestBody(hosts []string, query VWorkloadPreprocessResponse) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		op.nmaWorkloadReplayRequestData.Request = query.VParsedQuery
		op.nmaWorkloadReplayRequestData.StmtType = query.VStmtType
		op.nmaWorkloadReplayRequestData.FileName = query.VFileName
		op.nmaWorkloadReplayRequestData.JobID = op.JobID

		if query.VFileDir != "" {
			op.nmaWorkloadReplayRequestData.FileDir = op.hostNodeMap[host].CatalogPath
		} else {
			op.nmaWorkloadReplayRequestData.FileDir = query.VFileDir
		}

		dataBytes, err := json.Marshal(op.nmaWorkloadReplayRequestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaWorkloadReplayOp) setupClusterHTTPRequest(hosts []string, timeout int) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("workload-replay/replay")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		httpRequest.Timeout = timeout
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

// Validate the original workload data can actually be replayed without errors.
// No empty queries, incorrect date formatting, etc.
func (op *nmaWorkloadReplayOp) validateOriginalWorkloadData() error {
	originalData := op.workloadReplayData.originalWorkloadData

	for index, workloadQuery := range originalData {
		// Check for invalid start timestamp
		_, err := parseWorkloadTime(workloadQuery.StartTimestamp)
		if err != nil {
			return fmt.Errorf("invalid start timestamp at workload query index %d: %w", index, err)
		}

		if workloadQuery.Request == "" {
			return fmt.Errorf("empty request at index %d", index)
		}

		if workloadQuery.NodeName == "" {
			return fmt.Errorf("empty node name at index %d", index)
		}
	}

	return nil
}

func (op *nmaWorkloadReplayOp) prepare(execContext *opEngineExecContext) error {
	err := op.validateOriginalWorkloadData()
	if err != nil {
		return fmt.Errorf("invalid workload file data: %w", err)
	}

	op.workloadReplayData.replayData = []workloadQuery{}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts, 0)
}

// Set up HTTP request for a particular query
func (op *nmaWorkloadReplayOp) prepareRequest(originalQuery *workloadQuery, query VWorkloadPreprocessResponse) error {
	err := op.updateRequestBody(op.hosts, query)
	if err != nil {
		return err
	}

	// Queries can take a long time to complete, so increase the request timeout if necessary
	// Use either the default (5 minutes) or 1.5x original captured duration, whichever is higher
	timeoutMultiplier := 1.5
	originalRequestDurationTimeout := math.Floor(float64(originalQuery.RequestDurationMS) / 1000 * timeoutMultiplier)
	timeout := math.Max(defaultRequestTimeout, originalRequestDurationTimeout)

	// Convert timeout from int64 to int
	timeoutInt := 0
	if timeout <= math.MaxInt { // Max int in seconds is billions of years. This should never be false
		timeoutInt = int(timeout)
	}
	op.logger.Log.Info(fmt.Sprintf("Using timeout of %d seconds", timeoutInt), "name", op.name)

	return op.setupClusterHTTPRequest(op.hosts, timeoutInt)
}

func parseWorkloadTime(timestamp string) (time.Time, error) {
	const dateFormat = "2006-01-02T15:04:05.999999-07:00"
	parsedTime, err := time.Parse(dateFormat, timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("fail to parse workload timestamp: %w", err)
	}
	return parsedTime, nil
}

// In the event we hit an error during workload replay, append a row to the replay results containing the error detail
func (op *nmaWorkloadReplayOp) appendReplayErrorRow(err error) {
	errorRow := workloadQuery{
		ErrorDetails: err.Error(),
	}
	op.workloadReplayData.replayData = append(op.workloadReplayData.replayData, errorRow)
}

// Execute workload replay - replay a set of queries with the same relative timing
func (op *nmaWorkloadReplayOp) executeWorkloadReplay(execContext *opEngineExecContext) error {
	originalData := op.workloadReplayData.originalWorkloadData
	if len(originalData) == 0 {
		return nil
	}

	// We want to replay queries with the same relative timing as the original captured workload (or as close as we
	// can get), so keep track of timing relative to when workload replay starts
	originalStartTime, err := parseWorkloadTime(originalData[0].StartTimestamp)
	if err != nil {
		return err
	}
	replayStartTime := time.Now()

	op.logger.Log.Info("Started executing workload replay", "name", op.name)
	for index, workloadQuery := range originalData {
		select {
		case <-execContext.ctx.Done():
			op.logger.Log.Info("%s: Workload replay canceled: %v", op.name, execContext.ctx.Err())
			return execContext.ctx.Err()
		default:
			replayProgress := fmt.Sprintf("%d/%d", index, len(originalData))

			// Determine if we're behind or ahead of schedule
			originalQueryStartTime, err := parseWorkloadTime(workloadQuery.StartTimestamp)
			if err != nil {
				return err // Shouldn't happen since we do validation ahead of time
			}
			originalElapsedTime := originalQueryStartTime.Sub(originalStartTime)
			currentElapsedTime := time.Since(replayStartTime)

			// If we're ahead of schedule, sleep
			if currentElapsedTime < originalElapsedTime {
				sleepDuration := originalElapsedTime - currentElapsedTime
				op.logger.Log.Info("Workload replay ahead of schedule, sleeping", "name",
					op.name, "progress", replayProgress, "duration", sleepDuration)
				select {
				case <-time.After(sleepDuration):
					// Sleep finished, continue to the next iteration
				case <-execContext.ctx.Done():
					op.logger.Log.Info("%s: Workload replay canceled during sleep: %v", op.name, execContext.ctx.Err())
					return execContext.ctx.Err()
				}
			} else {
				op.logger.Log.Info("Workload replay behind schedule, running next query", "name", op.name, "progress", replayProgress)
			}

			// Preprocess query
			w := &vWorkloadPreprocessCall{
				bodyParams: VWorkloadPreprocessParams{
					VRequest:     workloadQuery.Request,
					VCatalogPath: "/"}}
			err = w.PreprocessQuery()
			if err != nil {
				op.appendReplayErrorRow(err)
				continue
			}
			preprocessedQuery := w.getResponse()

			// Send request to NMA to run the query
			op.logger.Log.Info("Replaying query "+workloadQuery.Request, "name", op.name, "progress", replayProgress)
			err = op.prepareRequest(&workloadQuery, preprocessedQuery)
			if err != nil {
				op.appendReplayErrorRow(err)
				continue
			}

			err = op.runExecute(execContext)
			if err != nil {
				select {
				case <-execContext.ctx.Done():
					op.logger.Log.Info("%s: runExecute canceled: %v", op.name, execContext.ctx.Err())
					return execContext.ctx.Err()
				default:
					op.appendReplayErrorRow(err)
					continue
				}
			}

			// Process/store results of replayed query
			err = op.processResult(execContext)
			if err != nil {
				op.appendReplayErrorRow(err)
			}
		}
	}
	op.logger.Log.Info("Finished executing workload replay", "name", op.name)

	return nil
}

func (op *nmaWorkloadReplayOp) execute(execContext *opEngineExecContext) error {
	return op.executeWorkloadReplay(execContext)
}

func (op *nmaWorkloadReplayOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaWorkloadReplayOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		responseObj := workloadQuery{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// Save the replay data
		op.workloadReplayData.replayData = append(op.workloadReplayData.replayData, responseObj)
		return nil
	}

	return allErrs
}
