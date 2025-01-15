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

	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaStageCommandsOp struct {
	scrutinizeOpBase
	skipCollectLibs bool
}

type stageCommandsRequestData struct {
	CatalogPath        string `json:"catalog_path"`
	SkipCollectLibs    bool   `json:"skip_collect_libs,omitempty"`
	IncludeCatalogLibs bool   `json:"include_catalog_libs,omitempty"`
}

type stageCommandsResponseData struct {
	Name string `json:"name"`
}

func makeNMAStageCommandsOp(logger vlog.Printer,
	id, batch string,
	hosts []string,
	hostNodeNameMap, hostCatPathMap map[string]string,
	skipCollectLibs bool) (nmaStageCommandsOp, error) {
	// base members
	op := nmaStageCommandsOp{}
	op.name = "NMAStageCommandsOp"
	op.description = "Stage commands"
	op.logger = logger.WithName(op.name)
	op.hosts = hosts

	// scrutinize members
	op.id = id
	op.batch = batch
	op.hostNodeNameMap = hostNodeNameMap
	op.hostCatPathMap = hostCatPathMap
	op.httpMethod = PostMethod
	op.urlSuffix = "/commands"

	// custom members
	op.skipCollectLibs = skipCollectLibs

	// the caller is responsible for making sure hosts and maps match up exactly
	err := validateHostMaps(hosts, hostNodeNameMap, hostCatPathMap)
	return op, err
}

func (op *nmaStageCommandsOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string, len(hosts))
	for i, host := range hosts {
		stageCommandsData := stageCommandsRequestData{}
		stageCommandsData.CatalogPath = op.hostCatPathMap[host]
		stageCommandsData.SkipCollectLibs = op.skipCollectLibs
		if i == 0 {
			// on one host, we collect all .so files in the catalog/Libraries directory
			stageCommandsData.IncludeCatalogLibs = true
		}

		dataBytes, err := json.Marshal(stageCommandsData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaStageCommandsOp) prepare(execContext *opEngineExecContext) error {
	err := op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaStageCommandsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaStageCommandsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaStageCommandsOp) processResult(_ *opEngineExecContext) error {
	fileList := make([]stageCommandsResponseData, 0)
	return processStagedItemsResult(&op.scrutinizeOpBase, fileList)
}
