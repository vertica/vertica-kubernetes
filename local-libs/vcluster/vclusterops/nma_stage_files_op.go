/*
 (c) Copyright [2024] Open Text.
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

type nmaStageFilesOp struct {
	scrutinizeOpBase
	logSizeLimitBytes int64 // maximum file size in bytes for any individual file
}

type stageFilesRequestData struct {
	CatalogPath       string `json:"catalog_path"`
	LogSizeLimitBytes int64  `json:"log_size_limit_bytes"`
}

type stageFilesResponseData struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

func makeNMAStageFilesOp(
	id, batch string,
	hosts []string,
	hostNodeNameMap map[string]string,
	hostCatPathMap map[string]string,
	logSizeLimitBytes int64) (nmaStageFilesOp, error) {
	// base members
	op := nmaStageFilesOp{}
	op.name = "NMAStageFilesOp"
	op.description = "Stage files"
	op.hosts = hosts

	// scrutinize members
	op.id = id
	op.batch = batch
	op.hostNodeNameMap = hostNodeNameMap
	op.hostCatPathMap = hostCatPathMap
	op.httpMethod = PostMethod
	op.urlSuffix = "/files"

	// custom members
	op.logSizeLimitBytes = logSizeLimitBytes

	// the caller is responsible for making sure hosts and maps match up exactly
	err := validateHostMaps(hosts, hostNodeNameMap, hostCatPathMap)
	return op, err
}

func (op *nmaStageFilesOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string, len(hosts))
	for _, host := range hosts {
		stageFilesData := stageFilesRequestData{}
		stageFilesData.CatalogPath = op.hostCatPathMap[host]
		stageFilesData.LogSizeLimitBytes = op.logSizeLimitBytes

		dataBytes, err := json.Marshal(stageFilesData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaStageFilesOp) prepare(execContext *opEngineExecContext) error {
	err := op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaStageFilesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaStageFilesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaStageFilesOp) processResult(_ *opEngineExecContext) error {
	fileList := make([]stageFilesResponseData, 0)
	return processStagedItemsResult(&op.scrutinizeOpBase, fileList)
}
