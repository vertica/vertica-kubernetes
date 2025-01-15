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

type nmaGetConfigurationParameterOp struct {
	opBase
	hostRequestBody     string
	sandbox             string
	initiator           string
	retrievedParamValue *string
}

type getConfigurationParameterData struct {
	sqlEndpointData
	ConfigParameter string `json:"config_parameter"`
	Level           string `json:"level"`
}

func makeNMAGetConfigurationParameterOp(hosts []string,
	username, dbName, sandbox, configParameter, level string, retrievedParamValue *string, /* out parameter */
	password *string, useHTTPPassword bool) (nmaGetConfigurationParameterOp, error) {
	op := nmaGetConfigurationParameterOp{}
	op.name = "NMAGetConfigurationParameterOp"
	op.description = "Get configuration parameter value"
	op.hosts = hosts
	op.sandbox = sandbox
	if retrievedParamValue == nil {
		return op, errors.New("argument retrievedParamValue cannot be a nil pointer")
	}
	op.retrievedParamValue = retrievedParamValue

	err := op.setupRequestBody(username, dbName, configParameter, level, password, useHTTPPassword)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaGetConfigurationParameterOp) setupRequestBody(
	username, dbName, configParameter, level string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	getConfigData := getConfigurationParameterData{}
	getConfigData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	getConfigData.ConfigParameter = configParameter
	getConfigData.Level = level

	dataBytes, err := json.Marshal(getConfigData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaGetConfigurationParameterOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("configuration/get")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaGetConfigurationParameterOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorInCluster(op.sandbox, op.hosts, execContext.upHostsToSandboxes)
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaGetConfigurationParameterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetConfigurationParameterOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetConfigurationParameterOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			genericResponse, err := op.parseAndCheckGenericJSONResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
			*op.retrievedParamValue = genericResponse.RespStr
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
