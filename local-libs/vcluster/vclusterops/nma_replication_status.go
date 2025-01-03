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

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaReplicationStatusOp struct {
	opBase
	nmaReplicationStatusRequestData
	TargetHosts        []string
	hostRequestBodyMap map[string]string
	transactionIDs     *[]int64
	replicationStatus  *[]ReplicationStatusResponse
}

func makeNMAReplicationStatusOp(targetHosts []string, targetUsePassword bool,
	replicationStatusData *nmaReplicationStatusRequestData,
	transactionIDs *[]int64, replicationStatus *[]ReplicationStatusResponse) (nmaReplicationStatusOp, error) {
	op := nmaReplicationStatusOp{}
	op.name = "NMAReplicationStatusOp"
	op.description = "Get asynchronous replication status"
	op.TargetHosts = targetHosts
	op.nmaReplicationStatusRequestData = *replicationStatusData
	op.transactionIDs = transactionIDs
	op.replicationStatus = replicationStatus

	if targetUsePassword {
		err := util.ValidateUsernameAndPassword(op.name, targetUsePassword, replicationStatusData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = replicationStatusData.UserName
		op.Password = replicationStatusData.Password
	}

	return op, nil
}

type nmaReplicationStatusRequestData struct {
	DBName                 string  `json:"dbname"`
	ExcludedTransactionIDs []int64 `json:"excluded_txn_ids,omitempty"`
	GetTransactionIDsOnly  bool    `json:"get_txn_ids_only,omitempty"`
	TransactionID          int64   `json:"txn_id,omitempty"`
	UserName               string  `json:"username"`
	Password               *string `json:"password"`
}

func (op *nmaReplicationStatusOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		dataBytes, err := json.Marshal(op.nmaReplicationStatusRequestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaReplicationStatusOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("replicate/status")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaReplicationStatusOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.TargetHosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.TargetHosts)

	return op.setupClusterHTTPRequest(op.TargetHosts)
}

func (op *nmaReplicationStatusOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaReplicationStatusOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaReplicationStatusOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	transactionIDs := mapset.NewSet[int64]()
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

		responseObj := []ReplicationStatusResponse{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// Get a list of transaction IDs. This can be used to retrieve a transaction ID when replication is started
		for _, replicationStatus := range responseObj {
			transactionIDs.Add(replicationStatus.TransactionID)
		}

		// If we're here, we've successfully received a status from one of the target hosts.
		// We don't need to check responses from other hosts as they should be the same
		if op.transactionIDs != nil {
			*op.transactionIDs = transactionIDs.ToSlice()
		}
		if op.replicationStatus != nil {
			*op.replicationStatus = responseObj
		}

		return nil
	}

	return allErrs
}
