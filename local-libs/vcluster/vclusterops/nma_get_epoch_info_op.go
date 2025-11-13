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
)

type nmaEpochInfoOp struct {
	opBase
	nmaEpochInfoRequestData
	hostCatalogPathMap map[string]string
	hosts              []string
	epoch              *[]EpochInfo
}

type nmaEpochInfoRequestData struct {
	CatalogPath string `json:"catalog_path"`
}

// This op is used to fetch epoch information from Epoch.log
func makeNMAEpochInfoOp(hosts []string,
	epochInfoData *nmaEpochInfoRequestData, epochInfo *[]EpochInfo,
	hostCatPathMap map[string]string) nmaEpochInfoOp {
	op := nmaEpochInfoOp{}
	op.name = "NMAEpochInfoOp"
	op.description = "Run get epoch info"
	op.hosts = hosts
	op.nmaEpochInfoRequestData = *epochInfoData
	op.epoch = epochInfo
	op.hostCatalogPathMap = hostCatPathMap
	return op
}

func (op *nmaEpochInfoOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		epochInfoData := nmaEpochInfoRequestData{}
		epochInfoData.CatalogPath = op.hostCatalogPathMap[host]
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("epoch-info")

		dataBytes, err := json.Marshal(epochInfoData)
		if err != nil {
			return fmt.Errorf("[%s] failed to marshal request data to JSON string: %w", op.name, err)
		}

		requestData := string(dataBytes)
		httpRequest.RequestData = requestData
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaEpochInfoOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaEpochInfoOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaEpochInfoOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaEpochInfoOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	var collectedEpochs []EpochInfo

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			allErrs = errors.Join(allErrs, fmt.Errorf("[%s] wrong certificate for NMA service on host %s", op.name, host))
			continue
		}

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		var responseObj []EpochInfo
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}
		op.logger.PrintInfo("[%s] response from host %s: %v", op.name, host, result.content)

		// Append epoch data from this host to the collection
		if len(responseObj) > 0 {
			op.logger.PrintInfo("Collected epoch info from host %s: %+v", host, responseObj[0])
			collectedEpochs = append(collectedEpochs, responseObj...)
		}
	}

	*op.epoch = collectedEpochs

	if len(collectedEpochs) == 0 && allErrs != nil {
		return fmt.Errorf("failed to get epoch info from any host: %w", allErrs)
	}

	return nil
}
