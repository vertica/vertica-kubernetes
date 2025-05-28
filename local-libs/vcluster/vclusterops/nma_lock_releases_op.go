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

type nmaLockReleasesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	startTime          string
	endTime            string
	nodeName           string
	resultLimit        int
	duration           string
}

type lockReleasesRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNMALockReleasesOp(upHosts []string, userName string,
	dbName string, password *string,
	startTime, endTime, nodeName string,
	resultLimit int, duration string) (nmaLockReleasesOp, error) {
	op := nmaLockReleasesOp{}
	op.name = "NMALockReleasesOp"
	op.description = "Check lock holding events"
	op.hosts = upHosts[:1] // set up the request for one of the up hosts only
	if duration == "" {
		op.duration = lockReleaseThresHold
	} else {
		op.duration = duration
	}
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

func (op *nmaLockReleasesOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := lockReleasesRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
		requestData.Params = make(map[string]any)
		requestData.Params["start-time"] = op.startTime
		requestData.Params["end-time"] = op.endTime
		if op.nodeName != "" {
			requestData.Params["node-name"] = op.nodeName
		}
		requestData.Params["object-name"] = lockObjectName
		requestData.Params["mode"] = "X"
		requestData.Params["duration"] = op.duration
		requestData.Params["limit"] = op.resultLimit
		requestData.Params["orderby"] = "duration DESC"

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaLockReleasesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("dc/lock-releases")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaLockReleasesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaLockReleasesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaLockReleasesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcLockReleases struct {
	Duration   string `json:"duration"`
	NodeName   string `json:"node_name"`
	Object     string `json:"object"`
	ObjectName string `json:"object_name"`
	SessionID  string `json:"session_id"`
	GrantTime  string `json:"grant_time"`
	Time       string `json:"time"`
	TxnID      string `json:"transaction_id"`
	UserID     string `json:"user_id"`
	UserName   string `json:"user_name"`
	// TODO: as for now http client not support retrieving batch results for txn info and session info
	// To improve the performance, we move the txn info and session info to the demond request by UI
	// when we migrate to use nma client, we can retrieve the txn info and session info in batch request
	// TxnInfo     dcTransactionStart `json:"transaction_info"`
	// SessionInfo dcSessionStart     `json:"session_info"`
}

func (event *dcLockReleases) getTxnID() string {
	return event.TxnID
}

func (event *dcLockReleases) getSessionID() string {
	return event.SessionID
}

func (op *nmaLockReleasesOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// for any passing result, directly return
		if result.isPassing() {
			var lockReleasesList []dcLockReleases
			err := op.parseAndCheckResponse(host, result.content, &lockReleasesList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			execContext.dcLockReleasesList = &lockReleasesList
			return nil
		}

		// record the error in failed results
		allErrs = errors.Join(allErrs, result.err)
	}

	return allErrs
}
