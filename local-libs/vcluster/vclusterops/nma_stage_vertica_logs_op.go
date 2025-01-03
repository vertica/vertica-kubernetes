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

type nmaStageVerticaLogsOp struct {
	scrutinizeOpBase
	logSizeLimitBytes int64
	logAgeMaxHours    int // The maximum age of archived logs in hours to retrieve
	logAgeMinHours    int // The minimum age of archived logs in hours to retrieve
}

type stageVerticaLogsRequestData struct {
	CatalogPath       string `json:"catalog_path"`
	LogSizeLimitBytes int64  `json:"log_size_limit_bytes"`
	LogAgeMaxHours    int    `json:"log_max_age_hours,omitempty"`
	LogAgeMinHours    int    `json:"log_min_age_hours,omitempty"`
}

type stageVerticaLogsResponseData struct {
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	ModTime   string `json:"mod_time"`
}

func makeNMAStageVerticaLogsOp(
	id string,
	hosts []string,
	hostNodeNameMap, hostCatPathMap map[string]string,
	logSizeLimitBytes int64,
	logAgeMaxHours, logAgeMinHours int) (nmaStageVerticaLogsOp, error) {
	// base members
	op := nmaStageVerticaLogsOp{}
	op.name = "NMAStageVerticaLogsOp"
	op.description = "Stage Vertica logs"
	op.hosts = hosts
	// scrutinize members
	op.id = id
	op.batch = scrutinizeBatchNormal
	op.hostNodeNameMap = hostNodeNameMap
	op.hostCatPathMap = hostCatPathMap
	op.httpMethod = PostMethod
	op.urlSuffix = "/vertica.log"

	// custom members
	op.logSizeLimitBytes = logSizeLimitBytes
	op.logAgeMaxHours = logAgeMaxHours
	op.logAgeMinHours = logAgeMinHours

	// the caller is responsible for making sure hosts and maps match up exactly
	err := validateHostMaps(hosts, hostNodeNameMap, hostCatPathMap)
	return op, err
}

func (op *nmaStageVerticaLogsOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string, len(hosts))
	for _, host := range hosts {
		stageVerticaLogsData := stageVerticaLogsRequestData{}
		stageVerticaLogsData.CatalogPath = op.hostCatPathMap[host]
		stageVerticaLogsData.LogSizeLimitBytes = op.logSizeLimitBytes
		stageVerticaLogsData.LogAgeMaxHours = op.logAgeMaxHours
		stageVerticaLogsData.LogAgeMinHours = op.logAgeMinHours

		dataBytes, err := json.Marshal(stageVerticaLogsData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *nmaStageVerticaLogsOp) prepare(execContext *opEngineExecContext) error {
	err := op.setupRequestBody(op.hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaStageVerticaLogsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaStageVerticaLogsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaStageVerticaLogsOp) processResult(_ *opEngineExecContext) error {
	fileList := make([]stageVerticaLogsResponseData, 0)
	return processStagedItemsResult(&op.scrutinizeOpBase, fileList)
}
