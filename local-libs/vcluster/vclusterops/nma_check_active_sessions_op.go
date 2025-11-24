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
)

// Gets a list of active sessions from NMA
// Will optionally check to make sure there are no active sessions before proceeding with further ops
type nmaCheckActiveSessionsOp struct {
	opBase
	hosts                  []string
	hostRequestBodyMap     map[string]string
	activeSessions         *[]ActiveSessionDetails
	assertNoActiveSessions bool
}

func makeNMACheckActiveSessionsOp(hosts []string, dbName string, userName string, password *string,
	activeSessions *[]ActiveSessionDetails, assertNoActiveSessions bool) (nmaCheckActiveSessionsOp, error) {
	op := nmaCheckActiveSessionsOp{}
	op.name = "nmaCheckActiveSessionsOp"
	op.description = "Check for active sessions"
	op.hosts = hosts
	op.activeSessions = activeSessions
	op.assertNoActiveSessions = assertNoActiveSessions

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

type checkActiveSessionsRequestData struct {
	sqlEndpointData
}

func (op *nmaCheckActiveSessionsOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := checkActiveSessionsRequestData{}
		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaCheckActiveSessionsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("connections/active/details")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaCheckActiveSessionsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaCheckActiveSessionsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaCheckActiveSessionsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

var ErrActiveSessions = fmt.Errorf("there are active sessions")

func (op *nmaCheckActiveSessionsOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		responseObj := []ActiveSessionDetails{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// "return" value via pointer
		*op.activeSessions = responseObj

		// Throw an error if we're not expecting any active sessions
		// There will always be 1 when NMA connects to run the query
		if op.assertNoActiveSessions && len(responseObj) > 1 {
			return ErrActiveSessions
		}
		return nil
	}

	return allErrs
}
