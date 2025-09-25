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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaMissingLockReleasesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	startTime          string
	endTime            string
	isDebug            bool
}

type missingLockReleasesRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNMAMissingLockReleasesOp(upHosts []string, userName string,
	dbName string, password *string,
	startTime, endTime string, isDebug bool) (nmaMissingLockReleasesOp, error) {
	op := nmaMissingLockReleasesOp{}
	op.name = "NMAMissingLockReleasesOp"
	op.description = "Check missing lock release events"
	op.hosts = upHosts[:1] // set up the request for one of the up hosts only
	op.startTime = startTime
	op.endTime = endTime
	op.isDebug = isDebug

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

func (op *nmaMissingLockReleasesOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := missingLockReleasesRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
		requestData.Params = make(map[string]any)
		if op.startTime != "" {
			requestData.Params["start-time"] = op.startTime
		}
		if op.endTime != "" {
			requestData.Params["end-time"] = op.endTime
		}
		if op.isDebug {
			requestData.Params["debug"] = util.TrueStr
		} else {
			requestData.Params["debug"] = util.FalseStr
		}

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaMissingLockReleasesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("dc/missing-releases")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaMissingLockReleasesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.opBase = op.opBase
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetitive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaMissingLockReleasesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaMissingLockReleasesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type MissingLockReleases struct {
	TxnID     string `json:"transaction_id"`
	SessionID string `json:"session_id"`
	UserName  string `json:"user_name"`
	StartTime string `json:"start_time"`
	WaitTime  string `json:"wait_time"`
	// TODO: fill HoldTime
	HoldTime    string `json:"hold_time"`
	Mode        string `json:"mode"`
	Scope       string `json:"scope"`
	ObjectName  string `json:"object_name"`
	NodeName    string `json:"node_name"`
	Description string `json:"description"`
	// TODO: fill TxnInfo
	TxnInfo *dcTransactionStarts `json:"transaction_info"`
}

func (op *nmaMissingLockReleasesOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// for any passing result, directly return
		if result.isPassing() {
			var missingLockReleasesList []MissingLockReleases
			err := op.parseAndCheckResponse(host, result.content, &missingLockReleasesList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			execContext.dcMissingReleasesList = &missingLockReleasesList
			return nil
		}

		// record the error in failed results
		allErrs = errors.Join(allErrs, result.err)
	}

	return allErrs
}
