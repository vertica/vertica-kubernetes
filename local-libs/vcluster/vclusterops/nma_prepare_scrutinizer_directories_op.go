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

	"github.com/vertica/vcluster/vclusterops/vlog"
	"golang.org/x/exp/maps"
)

type nmaPrepareScrutinizeDirectoriesOp struct {
	scrutinizeOpBase
	dirSuffix  string
	stagingDir *string
}

type prepareScrutinizeDirectoriesRequestData struct {
	Suffix string `json:"suffix"`
}

func makeNMAPrepareScrutinizeDirectoriesOp(logger vlog.Printer,
	id string,
	hostNodeNameMap map[string]string,
	batch string,
	suffix string,
	stagingDir *string) (nmaPrepareScrutinizeDirectoriesOp, error) {
	op := nmaPrepareScrutinizeDirectoriesOp{}
	op.name = "NMAPrepareScrutinizeDirectoriesOp"
	op.description = "Create necessary directories for scrutinize"
	op.logger = logger.WithName(op.name)
	op.id = id
	op.batch = batch
	op.dirSuffix = suffix
	op.hostNodeNameMap = hostNodeNameMap
	op.hosts = maps.Keys(hostNodeNameMap)
	op.stagingDir = stagingDir
	op.urlSuffix = "/directory"
	op.httpMethod = PostMethod

	err := op.setupRequestBody(hostNodeNameMap)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaPrepareScrutinizeDirectoriesOp) setupRequestBody(hostNodeNameMap map[string]string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for host := range hostNodeNameMap {
		prepareDirData := prepareScrutinizeDirectoriesRequestData{}
		prepareDirData.Suffix = op.dirSuffix

		dataBytes, err := json.Marshal(prepareDirData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}
	op.logger.Info("request data", "op name", op.name, "hostRequestBodyMap", op.hostRequestBodyMap)

	return nil
}

func (op *nmaPrepareScrutinizeDirectoriesOp) prepare(execContext *opEngineExecContext) error {
	host := getInitiatorFromUpHosts(execContext.upHosts, op.hosts)
	if host == "" {
		op.logger.PrintWarning("no up hosts among user specified hosts to collect system tables from, skipping the operation")
		op.skipExecute = true
		return nil
	}

	// construct host list for interface purposes
	op.hosts = []string{host}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaPrepareScrutinizeDirectoriesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaPrepareScrutinizeDirectoriesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

type prepareScrutinizeDirsResp struct {
	StagingDir string `json:"staging_dir"`
}

func (op *nmaPrepareScrutinizeDirectoriesOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			resp := prepareScrutinizeDirsResp{}
			err := op.parseAndCheckResponse(host, result.content, &resp)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
			*op.stagingDir = resp.StagingDir
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
