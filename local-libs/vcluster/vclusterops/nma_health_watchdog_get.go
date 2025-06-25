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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaHealthWatchdogGetOp struct {
	opBase
	nmaHealthWatchdogGetData
	hosts                      []string
	hostRequestBodyMap         map[string]string
	healthWatchdogValuesByHost *[]HealthWatchdogHostValue
}

func makeHealthWatchdogGetOp(hosts []string, usePassword bool,
	healthWatchdogGetData *nmaHealthWatchdogGetData,
	retrievedValues *[]HealthWatchdogHostValue) (nmaHealthWatchdogGetOp, error) {
	op := nmaHealthWatchdogGetOp{}
	op.name = "NMAHealthWatchdogGetOp"
	op.description = "Get health watchdog value"
	op.hosts = hosts
	op.nmaHealthWatchdogGetData = *healthWatchdogGetData
	op.healthWatchdogValuesByHost = retrievedValues

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, healthWatchdogGetData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = healthWatchdogGetData.UserName
		op.Password = healthWatchdogGetData.Password
	}

	return op, nil
}

// Request data to be sent to NMA health watchdog get endpoint
type nmaHealthWatchdogGetData struct {
	DBName     string  `json:"dbname"`
	UserName   string  `json:"username"`
	Password   *string `json:"password"`
	ModuleName string  `json:"module_name"`
	Action     string  `json:"action"`
}

type nmaHealthWatchdogGetResponse struct {
	Rows []HealthWatchdogValue `json:"rows"`
}

// Create request body JSON string
func (op *nmaHealthWatchdogGetOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		dataBytes, err := json.Marshal(op.nmaHealthWatchdogGetData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaHealthWatchdogGetOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("health-watchdog/get")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaHealthWatchdogGetOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaHealthWatchdogGetOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaHealthWatchdogGetOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaHealthWatchdogGetOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	results := []HealthWatchdogHostValue{}
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

		responseObj := []nmaHealthWatchdogGetResponse{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// We always expect 1 row back, regardless of module/action
		if len(responseObj) != 1 || len(responseObj[0].Rows) != 1 {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// "return" value via pointer
		hostValue := HealthWatchdogHostValue{}
		hostValue.Host = host
		hostValue.Value = responseObj[0].Rows[0]
		results = append(results, hostValue)
	}
	*op.healthWatchdogValuesByHost = results

	return allErrs
}
