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

type nmaTransactionStartsOp struct {
	opBase
	hostRequestBodyMap map[string]string
	transactionID      string
	startTime          string
	endTime            string
}
type transactionStartsRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

const (
	transactionStartsURL = "dc/transaction-starts"
)

func makeNMATransactionStartsOp(upHosts []string, userName string, dbName string, password *string,
	transactionID, startTime, endTime string) (nmaTransactionStartsOp, error) {
	op := nmaTransactionStartsOp{}
	op.hosts = upHosts[:1]
	op.transactionID = transactionID
	op.startTime = startTime
	op.endTime = endTime
	op.name = "NMATransactionStartsOp"
	op.description = "Check transaction starts"

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

func (op *nmaTransactionStartsOp) setupRequestBody(username, dbName string, useDBPassword bool,
	password *string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range op.hosts {
		requestData := transactionStartsRequestData{}

		requestData.Params = make(map[string]any)
		if op.transactionID != "" {
			requestData.Params["transactions"] = op.transactionID
		}
		if op.startTime != "" {
			requestData.Params["start-time"] = op.startTime
		}
		if op.endTime != "" {
			requestData.Params["end-time"] = op.endTime
		}
		requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *nmaTransactionStartsOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint(transactionStartsURL)
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaTransactionStartsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaTransactionStartsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaTransactionStartsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcTransactionStarts struct {
	Time        string `json:"time"`
	NodeName    string `json:"node_name"`
	SessionID   string `json:"session_id"`
	UserName    string `json:"user_name"`
	TxnID       string `json:"transaction_id"`
	Description string `json:"description"`
}

func (op *nmaTransactionStartsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var transactionStartList []dcTransactionStarts
			err := op.parseAndCheckResponse(host, result.content, &transactionStartList)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}
			// we only need result from one host
			execContext.dcTransactionStarts = &transactionStartList
			return allErrs
		}
	}

	return allErrs
}
