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

type nmaMissingReleasesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	startTime          string
	endTime            string
	isDebug            bool
}

type missingReleasesRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNMAMissingReleasesOp(upHosts []string, userName string,
	dbName string, password *string,
	startTime, endTime string, isDebug bool) (nmaMissingReleasesOp, error) {
	op := nmaMissingReleasesOp{}
	op.name = "NMAMissingReleasesOp"
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

func (op *nmaMissingReleasesOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := missingReleasesRequestData{}

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
func (op *nmaMissingReleasesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("dc/missing-releases")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaMissingReleasesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetitive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaMissingReleasesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaMissingReleasesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaMissingReleasesOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		// for any passing result, directly return
		if result.isPassing() {
			var missingReleasesList []DcLockAttempts
			err := op.parseAndCheckResponse(host, result.content, &missingReleasesList)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			execContext.dcMissingReleasesList = &missingReleasesList
			return nil
		}

		// record the error in failed results
		allErrs = errors.Join(allErrs, result.err)
	}

	return allErrs
}
