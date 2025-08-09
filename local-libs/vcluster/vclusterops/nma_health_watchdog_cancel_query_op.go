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

type nmaHealthWatchdogCancelQueryOp struct {
	opBase
	nmaHealthWatchdogCancelQueryData
	hosts              []string
	hostRequestBodyMap map[string]string
	cancelQuery        *[]HealthWatchdogCancelQueryResponse
}

func makeHealthWatchdogCancelQueryOp(hosts []string, usePassword bool,
	healthWatchdogCancelQueryData *nmaHealthWatchdogCancelQueryData,
	healthWatchdogCancelQueryResp *[]HealthWatchdogCancelQueryResponse) (nmaHealthWatchdogCancelQueryOp, error) {
	op := nmaHealthWatchdogCancelQueryOp{}
	op.name = "NMAHealthWatchdogCancelQueryOp"
	op.description = "CancelQuery health watchdog"
	op.hosts = hosts
	op.nmaHealthWatchdogCancelQueryData = *healthWatchdogCancelQueryData
	op.cancelQuery = healthWatchdogCancelQueryResp

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, healthWatchdogCancelQueryData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = healthWatchdogCancelQueryData.UserName
		op.Password = healthWatchdogCancelQueryData.Password
	}

	return op, nil
}

// Request data to be sent to NMA health watchdog cancel-query endpoint
type nmaHealthWatchdogCancelQueryData struct {
	DBName   string                             `json:"dbname"`
	UserName string                             `json:"username"`
	Password *string                            `json:"password"`
	Sessions []HealthWatchdogCancelQueryOptions `json:"sessions"`
}

// Create request body JSON string
func (op *nmaHealthWatchdogCancelQueryOp) updateRequestBody(hosts []string) error {
	// create json payload.
	dataBytes, err := json.Marshal(op.nmaHealthWatchdogCancelQueryData)
	if err != nil {
		return fmt.Errorf("fail to marshal request data: %w", err)
	}
	requestBody := string(dataBytes)
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		op.hostRequestBodyMap[host] = requestBody
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaHealthWatchdogCancelQueryOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("health-watchdog/cancel-query")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaHealthWatchdogCancelQueryOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaHealthWatchdogCancelQueryOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaHealthWatchdogCancelQueryOp) finalize(_ *opEngineExecContext) error {
	return nil
}

// processResult processes the results from all hosts for the cluster-wide cancel query operation.
// It assumes the cancellation is successful for the entire cluster and returns immediately
// upon receiving the first valid success response from any single host. If all hosts
// fail, it returns an aggregated error.
func (op *nmaHealthWatchdogCancelQueryOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		// Collect errors from failed nodes and continue with next host.
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// parse the successful response from the host.
		// If parsing fails and continue with next host.
		var healthWatchdogResponse []HealthWatchdogCancelQueryResponse
		err := op.parseAndCheckResponse(host, result.content, &healthWatchdogResponse)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// "return" cancel-query via pointer
		// A valid response was received from one host.
		// Because cancellation is a cluster-wide action, return immediately.
		if op.cancelQuery != nil {
			*op.cancelQuery = healthWatchdogResponse
		}
		return nil
	}

	return allErrs
}
