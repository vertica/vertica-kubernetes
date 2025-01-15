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
	"fmt"
)

type nmaStageDCTablesOp struct {
	scrutinizeOpBase
}

type stageDCTablesRequestData struct {
	CatalogPath string `json:"catalog_path"`
}

type stageDCTablesResponseData struct {
	Name string `json:"name"`
}

func makeNMAStageDCTablesOp(
	id string,
	hosts []string,
	hostNodeNameMap map[string]string,
	hostCatPathMap map[string]string) (nmaStageDCTablesOp, error) {
	// base members
	op := nmaStageDCTablesOp{}
	op.name = "NMAStageDCTablesOp"
	op.description = "Stage DC tables"
	op.hosts = hosts

	// scrutinize members
	op.id = id
	op.batch = scrutinizeBatchNormal
	op.hostNodeNameMap = hostNodeNameMap
	op.hostCatPathMap = hostCatPathMap
	op.httpMethod = PostMethod
	op.urlSuffix = "/data_collector"

	// the caller is responsible for making sure hosts and maps match up exactly
	err := validateHostMaps(hosts, hostNodeNameMap, hostCatPathMap)
	return op, err
}

func (op *nmaStageDCTablesOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string, len(hosts))
	for _, host := range hosts {
		stageDCTablesData := stageDCTablesRequestData{}
		stageDCTablesData.CatalogPath = op.hostCatPathMap[host]

		dataBytes, err := json.Marshal(stageDCTablesData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaStageDCTablesOp) prepare(execContext *opEngineExecContext) error {
	err := op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaStageDCTablesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaStageDCTablesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaStageDCTablesOp) processResult(_ *opEngineExecContext) error {
	fileList := make([]stageDCTablesResponseData, 0)
	return processStagedItemsResult(&op.scrutinizeOpBase, fileList)
}
