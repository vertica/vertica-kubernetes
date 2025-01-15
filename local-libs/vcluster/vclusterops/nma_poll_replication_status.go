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
	"slices"

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaPollReplicationStatusOp struct {
	opBase
	TargetDB               DatabaseOptions
	hostRequestBodyMap     map[string]string
	sandbox                string
	vdb                    *VCoordinationDatabase
	existingTransactionIDs *[]int64
	newTransactionID       *int64
}

func makeNMAPollReplicationStatusOp(targetDBOpt *DatabaseOptions, targetUsePassword bool,
	sandbox string, vdb *VCoordinationDatabase, existingTransactionIDs *[]int64, newTransactionID *int64) (nmaPollReplicationStatusOp, error) {
	op := nmaPollReplicationStatusOp{}
	op.name = "NMAPollReplicationStatusOp"
	op.description = "Retrieve asynchronous replication transaction ID"
	op.TargetDB.DBName = targetDBOpt.DBName
	op.TargetDB.Hosts = targetDBOpt.Hosts
	op.sandbox = sandbox
	op.vdb = vdb
	op.existingTransactionIDs = existingTransactionIDs
	op.newTransactionID = newTransactionID
	op.TargetDB.UserName = targetDBOpt.UserName

	if targetUsePassword {
		err := util.ValidateUsernameAndPassword(op.name, targetUsePassword, targetDBOpt.UserName)
		if err != nil {
			return op, err
		}
		op.TargetDB.Password = targetDBOpt.Password
	}

	return op, nil
}

func (op *nmaPollReplicationStatusOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		requestData := nmaReplicationStatusRequestData{}
		requestData.DBName = op.TargetDB.DBName
		requestData.ExcludedTransactionIDs = *op.existingTransactionIDs
		requestData.GetTransactionIDsOnly = true
		requestData.TransactionID = 0
		requestData.UserName = op.TargetDB.UserName
		requestData.Password = op.TargetDB.Password

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaPollReplicationStatusOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("replicate/status")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaPollReplicationStatusOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.TargetDB.Hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.TargetDB.Hosts)

	return op.setupClusterHTTPRequest(op.TargetDB.Hosts)
}

func (op *nmaPollReplicationStatusOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaPollReplicationStatusOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaPollReplicationStatusOp) processResult(execContext *opEngineExecContext) error {
	err := pollState(op, execContext)
	if err != nil {
		return fmt.Errorf("error polling replication status, %w", err)
	}

	return nil
}

func (op *nmaPollReplicationStatusOp) getPollingTimeout() int {
	return OneMinute
}

func (op *nmaPollReplicationStatusOp) shouldStopPolling() (bool, error) {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return true, fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		responseObj := []ReplicationStatusResponse{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			return true, errors.Join(allErrs, err)
		}

		// We should only receive 1 new transaction ID.
		// More than 1 means multiple replication jobs were started at the same time.
		// If this happens, we can't determine which transaction ID belongs to which job.
		if len(responseObj) > 1 {
			return true, errors.Join(allErrs, fmt.Errorf("[%s] expects one transaction ID but retrieved %d: %+v",
				op.name, len(responseObj), responseObj))
		}

		// Stop polling if NMA responds with a single new transaction ID
		if len(responseObj) == 1 {
			newTransactionID := responseObj[0].TransactionID

			// The transaction ID should be new, i.e. not in the list of existing transaction IDs
			if slices.Contains(*op.existingTransactionIDs, newTransactionID) {
				return true, errors.Join(allErrs, fmt.Errorf("[%s] transaction ID already exists %d",
					op.name, newTransactionID))
			}

			*op.newTransactionID = newTransactionID
			return true, nil
		}

		// If we're here, we've successfully received a status from one of the target hosts and there are no new transaction IDs
		// Keep polling in this case
		return false, nil
	}

	return true, allErrs
}
