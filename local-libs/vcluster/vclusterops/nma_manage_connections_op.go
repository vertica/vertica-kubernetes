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
)

type nmaManageConnectionsOp struct {
	opBase
	hostRequestBody string
	sandbox         string
	action          ConnectionDrainingAction
	initiator       string
}

type manageConnectionsData struct {
	sqlEndpointData
	SubclusterName   string `json:"subclustername"`
	RedirectHostname string `json:"hostname"`
}

func makeNMAManageConnectionsOp(hosts []string,
	username, dbName, sandbox, subclusterName, redirectHostname string, action ConnectionDrainingAction,
	password *string, useHTTPPassword bool) (nmaManageConnectionsOp, error) {
	op := nmaManageConnectionsOp{}
	op.name = "NMAManageConnectionsOp"
	op.description = "Manage connections on Vertica hosts"
	op.hosts = hosts
	op.action = action
	op.sandbox = sandbox

	err := op.setupRequestBody(username, dbName, subclusterName, redirectHostname, password,
		useHTTPPassword)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaManageConnectionsOp) setupRequestBody(
	username, dbName, subclusterName, redirectHostname string, password *string,
	useDBPassword bool) error {
	err := ValidateSQLEndpointData(op.name,
		useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	manageConnData := manageConnectionsData{}
	manageConnData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	manageConnData.SubclusterName = subclusterName
	if op.action == ActionRedirect {
		manageConnData.RedirectHostname = redirectHostname
	}

	dataBytes, err := json.Marshal(manageConnData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaManageConnectionsOp) setupClusterHTTPRequest(initiator string, action ConnectionDrainingAction) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("connections/" + string(action))
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaManageConnectionsOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorInCluster(op.sandbox, op.hosts, execContext.upHostsToSandboxes)
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator, op.action)
}

func (op *nmaManageConnectionsOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaManageConnectionsOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaManageConnectionsOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckStringResponse(host, result.content)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
			}
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
