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

type nmaSetConfigurationParameterOp struct {
	opBase
	hostRequestBody string
	sandbox         string
	initiator       string
}

type setConfigurationParameterData struct {
	sqlEndpointData
	ConfigParameter string `json:"config_parameter"`
	Value           string `json:"value"`
	Level           string `json:"level"`
}

func makeNMASetConfigurationParameterOp(hosts []string,
	username, dbName, sandbox, configParameter, value, level string,
	password *string, useHTTPPassword bool) (nmaSetConfigurationParameterOp, error) {
	op := nmaSetConfigurationParameterOp{}
	op.name = "NMASetConfigurationParameterOp"
	op.description = "Set configuration parameter value"
	op.hosts = hosts
	op.sandbox = sandbox

	err := op.setupRequestBody(username, dbName, configParameter, value, level, password, useHTTPPassword)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaSetConfigurationParameterOp) setupRequestBody(
	username, dbName, configParameter, value, level string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	setConfigData := setConfigurationParameterData{}
	setConfigData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	setConfigData.ConfigParameter = configParameter
	setConfigData.Value = value
	setConfigData.Level = level

	dataBytes, err := json.Marshal(setConfigData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaSetConfigurationParameterOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PutMethod
	httpRequest.buildNMAEndpoint("configuration/set")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaSetConfigurationParameterOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorInCluster(op.sandbox, op.hosts, execContext.upHostsToSandboxes)
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaSetConfigurationParameterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSetConfigurationParameterOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaSetConfigurationParameterOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckStringResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
