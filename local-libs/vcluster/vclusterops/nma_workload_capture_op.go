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

	"github.com/vertica/vcluster/vclusterops/util"
)

type nmaWorkloadCaptureOp struct {
	opBase
	nmaWorkloadCaptureRequestData
	hosts                []string
	hostRequestBodyMap   map[string]string
	workloadFileLocation string
	capturedWorkload     *[]workloadQuery
}

func makeNMAWorkloadCaptureOp(hosts []string, usePassword bool,
	workloadCaptureData *nmaWorkloadCaptureRequestData, workloadFileLocation string,
	capturedWorkload *[]workloadQuery) (nmaWorkloadCaptureOp, error) {
	op := nmaWorkloadCaptureOp{}
	op.name = "NMAWorkloadCaptureOp"
	op.description = "Capture workload"
	op.hosts = hosts
	op.nmaWorkloadCaptureRequestData = *workloadCaptureData
	op.workloadFileLocation = workloadFileLocation
	op.capturedWorkload = capturedWorkload

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, workloadCaptureData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = workloadCaptureData.UserName
		op.Password = workloadCaptureData.Password
	}

	return op, nil
}

// Request data to be sent to NMA capture workload endpoint
type nmaWorkloadCaptureRequestData struct {
	DBName         string  `json:"dbname"`
	UserName       string  `json:"username"`
	Password       *string `json:"password"`
	StartTimestamp string  `json:"start_timestamp"`
	EndTimestamp   string  `json:"end_timestamp"`
}

// Create request body JSON string
func (op *nmaWorkloadCaptureOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		dataBytes, err := json.Marshal(op.nmaWorkloadCaptureRequestData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaWorkloadCaptureOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("workload-replay/capture")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaWorkloadCaptureOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaWorkloadCaptureOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaWorkloadCaptureOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaWorkloadCaptureOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

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

		responseObj := []workloadQuery{}
		err := op.parseAndCheckResponse(host, result.content, &responseObj)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// "return" captured workload via pointer
		if responseObj != nil {
			*op.capturedWorkload = responseObj
		}

		return nil
	}

	return allErrs
}
