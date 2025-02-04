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

type nmaCleanCommunalStorageOp struct {
	opBase
	hostRequestBody string
}

type cleanCommunalStorageData struct {
	sqlEndpointData
	infoOnly bool /*only report information about the files that will be cleaned*/
}

func makeNMACleanCommunalStorageOp(hosts []string, username, dbName string,
	password *string, useHTTPPassword, infoOnly bool) (nmaCleanCommunalStorageOp, error) {
	op := nmaCleanCommunalStorageOp{}
	op.name = "NMACleanCommunalStorageOp"
	op.description = "Clean communal storage"
	op.hosts = hosts

	err := op.setupRequestBody(username, dbName, password, useHTTPPassword, infoOnly)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaCleanCommunalStorageOp) setupRequestBody(username, dbName string,
	password *string, useDBPassword, infoOnly bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	requestData := cleanCommunalStorageData{}
	requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	requestData.infoOnly = infoOnly

	dataBytes, err := json.Marshal(requestData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaCleanCommunalStorageOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("communal-storage/clean")
		httpRequest.RequestData = op.hostRequestBody

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaCleanCommunalStorageOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaCleanCommunalStorageOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaCleanCommunalStorageOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaCleanCommunalStorageOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckGenericJSONResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
