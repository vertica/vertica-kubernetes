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
	userName           string
	startTime          string
	endTime            string
	nodeName           string
	resultLimit        int
	duration           string
}

type lockReleasesRequestData struct {
	Params   map[string]any `json:"params"`
	Username string         `json:"username"`
}

func makeNMALockReleasesOp(upHosts []string, userName string,
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
	op.userName = userName
	op.startTime = startTime
	op.endTime = endTime
	op.nodeName = nodeName
	op.resultLimit = resultLimit

	err := op.setupRequestBody()
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaLockReleasesOp) setupRequestBody() error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := lockReleasesRequestData{}

		requestData.Username = op.userName
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
	// TxnInfo and SessionInfo are not used for parsing data from the NMA endpoint
	// but will be used to show detailed info about the retrieved TxnID and SessionID
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
		op.logResponse(host, result)
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
