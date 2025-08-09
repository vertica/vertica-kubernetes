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
	SessionID          string
	StatementID        int64
	cancelQuery        *HealthWatchdogCancelQueryResponse
}

func makeHealthWatchdogCancelQueryOp(hosts []string, usePassword bool,
	healthWatchdogCancelQueryData *nmaHealthWatchdogCancelQueryData,
	healthWatchdogCancelQueryResp *HealthWatchdogCancelQueryResponse) (nmaHealthWatchdogCancelQueryOp, error) {
	op := nmaHealthWatchdogCancelQueryOp{}
	op.name = "NMAHealthWatchdogCancelQueryOp"
	op.description = "CancelQuery health watchdog"
	op.hosts = hosts
	op.nmaHealthWatchdogCancelQueryData = *healthWatchdogCancelQueryData
	op.SessionID = healthWatchdogCancelQueryData.SessionID
	op.StatementID = healthWatchdogCancelQueryData.StatementID
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
	DBName      string  `json:"dbname"`
	UserName    string  `json:"username"`
	Password    *string `json:"password"`
	SessionID   string  `json:"session_id"`
	StatementID int64   `json:"statement_id,omitempty"`
}

// Create request body JSON string
func (op *nmaHealthWatchdogCancelQueryOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		op.nmaHealthWatchdogCancelQueryData.SessionID = op.SessionID
		op.nmaHealthWatchdogCancelQueryData.StatementID = op.StatementID
		dataBytes, err := json.Marshal(op.nmaHealthWatchdogCancelQueryData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
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

func (op *nmaHealthWatchdogCancelQueryOp) processResult(_ *opEngineExecContext) error {
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

		healthWatchdogResponse := HealthWatchdogCancelQueryResponse{}
		err := op.parseAndCheckResponse(host, result.content, &healthWatchdogResponse)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// "return" cancel-query via pointer
		if op.cancelQuery != nil {
			*op.cancelQuery = healthWatchdogResponse
		}
		return nil
	}

	return allErrs
}
