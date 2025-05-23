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
	"encoding/json"
	"errors"
	"fmt"
)

type nmaLockAttemptsOp struct {
	opBase
	hostRequestBodyMap map[string]string
	startTime          string
	endTime            string
	nodeName           string
	resultLimit        int
}

type lockAttemptsRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

const lockObjectName = "Global Catalog"

// TODO: We should let the endpoint just accept the seconds
const minLockWaitDuration = "00:00:30"

//nolint:dupl // TODO all "SQL" endpoints could use a style and refactor pass
func makeNMALockAttemptsOp(upHosts []string, userName, dbName string,
	password *string, startTime, endTime, nodeName string,
	resultLimit int) (nmaLockAttemptsOp, error) {
	op := nmaLockAttemptsOp{}
	op.name = "NMALockAttemptsOp"
	op.description = "Check lock waiting events"
	op.hosts = upHosts[:1] // set up the request for one of the up hosts only
	op.startTime = startTime
	op.endTime = endTime
	op.nodeName = nodeName
	op.resultLimit = resultLimit

	// NMA endpoints don't need to differentiate between empty password and no password
	useDBPassword := password != nil
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, userName, password, dbName)
	if err != nil {
		return op, err
	}
	err = op.setupRequestBody(userName, dbName, useDBPassword, password)
	return op, err
}

func (op *nmaLockAttemptsOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := lockAttemptsRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
		requestData.Params = make(map[string]any)
		requestData.Params["start-time"] = op.startTime
		requestData.Params["end-time"] = op.endTime
		if op.nodeName != "" {
			requestData.Params["node-name"] = op.nodeName
		}
		requestData.Params["object-name"] = lockObjectName
		requestData.Params["mode"] = "X"
		requestData.Params["duration"] = minLockWaitDuration
		requestData.Params["limit"] = op.resultLimit

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaLockAttemptsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("dc/lock-attempts")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaLockAttemptsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaLockAttemptsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaLockAttemptsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcLockAttempts struct {
	Description string `json:"description"`
	Duration    string `json:"duration"`
	Mode        string `json:"mode"`
	NodeName    string `json:"node_name"`
	Object      string `json:"object"`
	ObjectName  string `json:"object_name"`
	SessionID   string `json:"session_id"`
	StartTime   string `json:"start_time"`
	Time        string `json:"time"`
	TxnID       string `json:"transaction_id"`
	// TxnInfo and SessionInfo are not used for parsing data from the NMA endpoint
	// but will be used to show detailed info about the retrieved TxnID and SessionID
	TxnInfo     dcTransactionStart `json:"transaction_info"`
	SessionInfo dcSessionStart     `json:"session_info"`
}

func (event *dcLockAttempts) getSessionID() string {
	return event.SessionID
}

func (event *dcLockAttempts) getTxnID() string {
	return event.TxnID
}

func (op *nmaLockAttemptsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		// for any passing result, directly return
		if result.isPassing() {
			var lockAttemptsList []dcLockAttempts
			err := op.parseAndCheckResponse(host, result.content, &lockAttemptsList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			execContext.dcLockAttemptsList = &lockAttemptsList
			return nil
		}

		// record the error in failed results
		allErrs = errors.Join(allErrs, result.err)
	}

	return allErrs
}
