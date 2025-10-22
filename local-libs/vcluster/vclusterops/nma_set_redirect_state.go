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

type nmaSetRedirectStateOp struct {
	opBase
	hostRequestBody string
	sandbox         string
	initiator       string
}

type RedirectStateRow struct {
	ID           string `json:"id"`
	SubclusterID int64  `json:"subcluster_id"`
	Start        string `json:"start"`
	Key          string `json:"key"`
}

type setRedirectStateData struct {
	sqlEndpointData
	Rows []RedirectStateRow `json:"rows"`
}

func makeNmaSetRedirectStateOp(hosts []string, username, dbName, sandbox string, password *string,
	useHTTPPassword bool, rows []RedirectStateRow) (nmaSetRedirectStateOp, error) {
	op := nmaSetRedirectStateOp{}
	op.name = "NmaSetRedirectStateOp"
	op.description = "Insert rows into v_redirect_state"
	op.hosts = hosts
	op.sandbox = sandbox

	err := op.setupRequestBody(username, dbName, password, useHTTPPassword, rows)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaSetRedirectStateOp) setupRequestBody(username, dbName string, password *string, useDBPassword bool,
	rows []RedirectStateRow) error {
	err := ValidateSQLEndpointData(op.name, useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	setRedirectStateData := setRedirectStateData{}
	setRedirectStateData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	setRedirectStateData.Rows = rows
	dataBytes, err := json.Marshal(setRedirectStateData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	return nil
}

func (op *nmaSetRedirectStateOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("redirect-state/set")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaSetRedirectStateOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorInCluster(op.sandbox, op.hosts, execContext.upHostsToSandboxes)
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaSetRedirectStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaSetRedirectStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaSetRedirectStateOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
