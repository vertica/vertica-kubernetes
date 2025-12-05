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

type nmaCloneSubclusterPropertiesOp struct {
	opBase
	hostRequestBodyMap map[string]string
	dbName             string
	hosts              []string
	sourceSubcluster   string
	targetSubcluster   string
}

type cloneSubclusterPropertiesRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

// makeNMACloneSubclusterPropertiesOp creates an operation to clone subcluster properties via NMA
func makeNMACloneSubclusterPropertiesOp(hosts []string, dbName string, userName string,
	password *string, sourceSubcluster string,
	targetSubcluster string) (nmaCloneSubclusterPropertiesOp, error) {
	op := nmaCloneSubclusterPropertiesOp{}
	op.name = "NMACloneSubclusterPropertiesOp"
	op.description = "Clone subcluster properties from source subcluster to target subcluster"
	op.hosts = hosts
	op.dbName = dbName
	op.sourceSubcluster = sourceSubcluster
	op.targetSubcluster = targetSubcluster

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

func (op *nmaCloneSubclusterPropertiesOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := cloneSubclusterPropertiesRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)

		// Add source and target subcluster as params
		requestData.Params = map[string]any{
			"source-subcluster": op.sourceSubcluster,
			"target-subcluster": op.targetSubcluster,
		}

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaCloneSubclusterPropertiesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("configuration/clone-subcluster-properties")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaCloneSubclusterPropertiesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaCloneSubclusterPropertiesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaCloneSubclusterPropertiesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type clonePropertiesResponse struct {
	Result string `json:"clone_subcluster_properties"`
}

func (op *nmaCloneSubclusterPropertiesOp) processResult(_ *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] processing clone subcluster properties result", op.name)

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

		var responseObj []clonePropertiesResponse
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}
		op.logger.PrintInfo("[%s] response: %v", op.name, result.content)

		if len(responseObj) == 0 {
			allErrs = errors.Join(allErrs, fmt.Errorf("[%s] empty response from host %s", op.name, host))
			continue
		}

		// Success
		op.logger.PrintInfo("[%s] successfully cloned properties from %s to %s",
			op.name, op.sourceSubcluster, op.targetSubcluster)
		return nil
	}

	return allErrs
}
