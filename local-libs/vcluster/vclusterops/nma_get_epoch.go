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

type nmaGetEpochOp struct {
	opBase
	hostRequestBodyMap map[string]string
	dbName             string
	hosts              []string
	epoch              int64
	returnEpoch        *[]ReturnEpochQuery
}

type epochRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

// This op is used to fetch the last good epoch from the database
func makeNMAGetEpochOp(hosts []string, dbName string, userName string,
	password *string, returnEpoch *[]ReturnEpochQuery) (nmaGetEpochOp, error) {
	op := nmaGetEpochOp{}
	op.name = "NMAReturnEpochOp"
	op.description = "Get epoch from database"
	op.hosts = hosts
	op.dbName = dbName
	op.returnEpoch = returnEpoch

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

func (op *nmaGetEpochOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := epochRequestData{}

		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaGetEpochOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("epoch")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaGetEpochOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaGetEpochOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetEpochOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type epochInfo struct {
	Epoch int64 `json:"GET_LAST_GOOD_EPOCH,string"`
}

func (op *nmaGetEpochOp) processResult(_ *opEngineExecContext) error {
	op.logger.PrintInfo("[%s] response: return epoch result processed", op.name)

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

		var responseObj []epochInfo
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

		epoch := responseObj[0].Epoch
		if op.returnEpoch != nil {
			*op.returnEpoch = []ReturnEpochQuery{{Epoch: epoch}}
			op.logger.PrintInfo("[%s] successfully retrieved epoch: %d", op.name, epoch)
		}
		op.epoch = epoch

		return nil
	}

	return allErrs
}
