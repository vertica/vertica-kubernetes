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

type clientSessions struct {
	Active int `json:"active_sessions"`
	Total  int `json:"total_sessions"`
}

type nmaActiveConnectionsOp struct {
	opBase
	requestBody string
	timeout     int
	allSessions bool
}

type nmaActiveConnectionsRequestData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNMAPollConnectionsOp(options *DatabaseOptions, subclusters []string, timeout int,
	allSessions bool) (nmaActiveConnectionsOp, error) {
	op := nmaActiveConnectionsOp{}
	op.name = "nmaActiveConnectionsOp"
	op.description = "Get active vertica connections"
	op.hosts = options.Hosts
	op.timeout = timeout
	op.allSessions = allSessions

	err := op.setupRequestBody(options.UserName, options.DBName, subclusters, options.Password, options.usePassword)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaActiveConnectionsOp) setupRequestBody(username, dbName string, subclusters []string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name, useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	requestData := nmaActiveConnectionsRequestData{}
	requestData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	requestData.Params = make(map[string]any)
	first := true
	subclustersStr := ""
	for _, id := range subclusters {
		if first {
			first = false
		} else {
			subclustersStr += ","
		}
		subclustersStr += id
	}
	requestData.Params["subclusters"] = subclustersStr
	dataBytes, err := json.Marshal(requestData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.requestBody = string(dataBytes)

	return nil
}

func (op *nmaActiveConnectionsOp) setupClusterHTTPRequest(hosts []string) error {
	initiator := getInitiator(hosts)
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("connections/active")
	httpRequest.RequestData = op.requestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest
	return nil
}

func (op *nmaActiveConnectionsOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaActiveConnectionsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaActiveConnectionsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaActiveConnectionsOp) processResult(execContext *opEngineExecContext) error {
	waitingFor := "paused"
	if op.allSessions {
		waitingFor = "redirected"
	}
	op.logger.PrintInfo("[%s] waiting for all connections to be %s", op.name, waitingFor)
	if err := pollState(op, execContext); err != nil {
		return fmt.Errorf("failed to wait for all connections to be %s; details %w", waitingFor, err)
	}

	return nil
}

func (op *nmaActiveConnectionsOp) getPollingTimeout() int {
	return op.timeout
}

func (op *nmaActiveConnectionsOp) shouldStopPolling() (bool, error) {
	var allErrs error
	var res []clientSessions

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			err := op.parseAndCheckResponse(host, result.content, &res)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}
	if allErrs != nil {
		return true, allErrs
	}
	if op.allSessions {
		return res[0].Total == 0, allErrs
	}
	return res[0].Active == 0, allErrs
}
