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

	"github.com/vertica/vcluster/vclusterops/util"
	"github.com/vertica/vcluster/vclusterops/vlog"
)

type nmaSaveRestorePointsOp struct {
	opBase
	saveRestorePointsRequestData
	sandbox string
}

type saveRestorePointsRequestData struct {
	DBName      string  `json:"dbname"`
	ArchiveName string  `json:"archive_name"`
	UserName    string  `json:"username"`
	Password    *string `json:"password"`
}

// This op is used to save restore points in a database
func makeNMASaveRestorePointsOp(logger vlog.Printer, hosts []string,
	saveRestorepointrequestData *saveRestorePointsRequestData, sandbox string,
	usePassword bool) (nmaSaveRestorePointsOp, error) {
	op := nmaSaveRestorePointsOp{}
	op.name = "NMASaveRestorePointsOp"
	op.description = "Run save restore point query"
	op.logger = logger.WithName("NMASaveRestorePointsOp")
	op.hosts = hosts
	op.saveRestorePointsRequestData = *saveRestorepointrequestData
	op.sandbox = sandbox

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, op.UserName)
		if err != nil {
			return op, err
		}
	}
	return op, nil
}

// make https json data
func (op *nmaSaveRestorePointsOp) setupRequestBody() (map[string]string, error) {
	hostRequestBodyMap := make(map[string]string, len(op.hosts))
	for _, host := range op.hosts {
		requestData := op.saveRestorePointsRequestData

		dataBytes, err := json.Marshal(requestData)
		if err != nil {
			return nil, fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}
		hostRequestBodyMap[host] = string(dataBytes)
	}
	return hostRequestBodyMap, nil
}

func (op *nmaSaveRestorePointsOp) setupClusterHTTPRequest(hostRequestBodyMap map[string]string) error {
	for host, requestBody := range hostRequestBodyMap {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("restore-points/save")
		httpRequest.RequestData = requestBody
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *nmaSaveRestorePointsOp) prepare(execContext *opEngineExecContext) error {
	hostRequestBody, err := op.setupRequestBody()
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(hostRequestBody)
}

func (op *nmaSaveRestorePointsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSaveRestorePointsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

/*
Sample response from the NMA restore-points endpoint:
RespStr: "" (status code:200)
*/
func (op *nmaSaveRestorePointsOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong certificate for NMA service on host %s",
				op.name, host)
		}
		if result.isPassing() {
			var responseObj RestorePoint
			err := op.parseAndCheckResponse(host, result.content, &responseObj)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}
			op.logger.PrintInfo("OP Name: [%s], response: %v", op.name, result.content)
			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return allErrs
}
