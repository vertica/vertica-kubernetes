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
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type httpsTransactionStartsOp struct {
	opBase
	transactionID string
	startTime     string
	endTime       string

	// when debug mode is on, this op will return stub data
	debug bool
}

const (
	transactionStartsURL = "dc/transaction-starts"
)

func makeHTTPSTransactionStartsOp(upHosts []string, transactionID, startTime, endTime string, debug bool) httpsTransactionStartsOp {
	op := httpsTransactionStartsOp{}
	op.name = "HTTPSTransactionStartsOp"
	op.description = "Check transaction starts"
	op.hosts = upHosts
	op.transactionID = transactionID
	op.startTime = startTime
	op.endTime = endTime
	op.debug = debug
	return op
}

// setupClusterHTTPRequest works as the module setup in Admintools
func (op *httpsTransactionStartsOp) setupClusterHTTPRequest(hosts []string) error {
	// this op may consume resources of the database,
	// thus we only need to send https request to one of the up hosts
	var queryParts []string
	baseURL := transactionStartsURL
	for _, host := range hosts[:1] {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		queryParams := make(map[string]string)

		if op.startTime != "" {
			queryParams["start-time"] = op.startTime
		}
		if op.endTime != "" {
			queryParams["end-time"] = op.endTime
		}
		if op.transactionID != "" {
			queryParams["txn-id"] = op.transactionID
		}

		for key, value := range queryParams {
			queryParts = append(queryParts, fmt.Sprintf("%s=%s", key, value))
		}

		// Join query parts to form a query string
		queryString := url.PathEscape(strings.Join(queryParts, "&"))
		httpRequest.buildHTTPSEndpoint(fmt.Sprintf("%s?%s", baseURL, queryString))

		// Save the request
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsTransactionStartsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	// Disable the spinner for this op as the op can be called multiple times.
	// This way would avoid repetive and confusing information.
	op.spinner = nil

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsTransactionStartsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsTransactionStartsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type dcTransactionStarts struct {
	TransactionStartsList []dcTransactionStart `json:"dc_transaction_starts_list"`
}

type dcTransactionStart struct {
	Time        string `json:"timestamp"`
	NodeName    string `json:"node_name"`
	SessionID   string `json:"session_id"`
	UserName    string `json:"user_name"`
	TxnID       string `json:"txn_id"`
	Description string `json:"description"`
}

func (op *httpsTransactionStartsOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var TransactionStarts dcTransactionStarts
			err := op.parseAndCheckResponse(host, result.content, &TransactionStarts)
			if err != nil {
				return errors.Join(allErrs, err)
			}

			// we only need result from one host
			execContext.dcTransactionStarts = &TransactionStarts
			return allErrs
		}
	}

	return allErrs
}
