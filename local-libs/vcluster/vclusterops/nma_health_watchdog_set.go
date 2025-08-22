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

type nmaHealthWatchdogSetOp struct {
	opBase
	nmaHealthWatchdogSetData
	hosts              []string
	hostRequestBodyMap map[string]string
}

// Request data to be sent to NMA health watchdog set endpoint
type nmaHealthWatchdogSetData struct {
	DBName         string            `json:"dbname"`
	UserName       string            `json:"username"`
	Password       *string           `json:"password"`
	ParameterName  string            `json:"parameter_name"`
	Action         string            `json:"action"`
	Value          string            `json:"value,omitempty"`
	PolicySettings map[string]string `json:"policy_settings,omitempty"`
}

func makeHealthWatchdogSetOp(hosts []string, usePassword bool,
	healthWatchdogSetData *nmaHealthWatchdogSetData) (nmaHealthWatchdogSetOp, error) {
	op := nmaHealthWatchdogSetOp{}
	op.name = "NMAHealthWatchdogSetOp"
	op.description = "Set health watchdog value"
	op.hosts = hosts
	op.nmaHealthWatchdogSetData = *healthWatchdogSetData

	if usePassword {
		err := util.ValidateUsernameAndPassword(op.name, usePassword, healthWatchdogSetData.UserName)
		if err != nil {
			return op, err
		}
		op.UserName = healthWatchdogSetData.UserName
		op.Password = healthWatchdogSetData.Password
	}

	return op, nil
}

// Create request body JSON string
func (op *nmaHealthWatchdogSetOp) updateRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		dataBytes, err := json.Marshal(op.nmaHealthWatchdogSetData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

// Set up HTTP requests to send to NMA hosts
func (op *nmaHealthWatchdogSetOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildNMAEndpoint("health-watchdog/set")
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaHealthWatchdogSetOp) prepare(execContext *opEngineExecContext) error {
	err := op.updateRequestBody(op.hosts)
	if err != nil {
		return err
	}

	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaHealthWatchdogSetOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaHealthWatchdogSetOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaHealthWatchdogSetOp) processResult(_ *opEngineExecContext) error {
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
	}
	return allErrs
}
