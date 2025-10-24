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
	"strconv"
)

type redirectStateRowResp struct {
	ID           string `json:"id"`
	SubclusterID string `json:"subcluster_id"`
	Start        string `json:"start"`
	Key          string `json:"key"`
}

type nmaGetRedirectStateOp struct {
	opBase
	hostRequestBody string
	sandbox         string
	initiator       string
	result          *[]RedirectStateRow
}

type getRedirectStateData struct {
	sqlEndpointData
	Params map[string]any `json:"params"`
}

func makeNmaGetRedirectStateOp(hosts []string, username, dbName, sandbox string, password *string,
	useHTTPPassword bool, excludeIDs []string, result *[]RedirectStateRow) (nmaGetRedirectStateOp, error) {
	op := nmaGetRedirectStateOp{}
	op.name = "NmaGetRedirectStateOp"
	op.description = "Get v_redirect_state rows"
	op.hosts = hosts
	op.sandbox = sandbox
	op.result = result

	err := op.setupRequestBody(username, dbName, password, useHTTPPassword, excludeIDs)
	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *nmaGetRedirectStateOp) setupRequestBody(username, dbName string, password *string, useDBPassword bool,
	excludeIDs []string) error {
	err := ValidateSQLEndpointData(op.name, useDBPassword, username, password, dbName)
	if err != nil {
		return err
	}
	getRedirectStateData := getRedirectStateData{}
	getRedirectStateData.sqlEndpointData = createSQLEndpointData(username, dbName, useDBPassword, password)
	getRedirectStateData.Params = make(map[string]any)
	if len(excludeIDs) > 0 {
		first := true
		excludeIDsStr := ""
		for _, id := range excludeIDs {
			if first {
				first = false
			} else {
				excludeIDsStr += ","
			}
			excludeIDsStr += id
		}
		getRedirectStateData.Params["exclude_ids"] = excludeIDsStr
	}
	dataBytes, err := json.Marshal(getRedirectStateData)
	if err != nil {
		return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
	}

	op.hostRequestBody = string(dataBytes)

	op.logger.Info("request data", "op name", op.name, "hostRequestBody", op.hostRequestBody)

	return nil
}

func (op *nmaGetRedirectStateOp) setupClusterHTTPRequest(initiator string) error {
	httpRequest := hostHTTPRequest{}
	// POST not GET for legacy (nma not using custom headers) reasons
	httpRequest.Method = PostMethod
	httpRequest.buildNMAEndpoint("redirect-state/get")
	httpRequest.RequestData = op.hostRequestBody
	op.clusterHTTPRequest.RequestCollection[initiator] = httpRequest

	return nil
}

func (op *nmaGetRedirectStateOp) prepare(execContext *opEngineExecContext) error {
	// select an up host in the sandbox or main cluster as the initiator
	initiator, err := getInitiatorInCluster(op.sandbox, op.hosts, execContext.upHostsToSandboxes)
	if err != nil {
		return err
	}
	op.initiator = initiator
	execContext.dispatcher.setup([]string{op.initiator})
	return op.setupClusterHTTPRequest(op.initiator)
}

func (op *nmaGetRedirectStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetRedirectStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetRedirectStateOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		if result.isPassing() {
			// decode the json-format response
			// a successful response will contain the cluster's redirect info
			/*
				[
					{
						"id: "0b4c2f1e-f6d3-4725-974d-8152a62e56c7",
						"subcluster_id"": "45035996273704988",
						"start": "2025-10-02 11:28:04.110594-04",
						"key": "b6e37650f986559a57f37e20452c35a26f01cc0a49ac4263d10caedbb9d992fd"
					},
					...
				]
			*/
			redirectStateRowResp := []redirectStateRowResp{}
			err := op.parseAndCheckResponse(host, result.content, redirectStateRowResp)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}
			for _, res := range redirectStateRowResp {
				scID, err := strconv.ParseInt(res.SubclusterID, 10, 64)
				if err != nil {
					allErrs = errors.Join(allErrs, err)
					continue
				}
				*op.result = append(*op.result, RedirectStateRow{res.ID, scID, res.Start, res.Key})
			}

			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return appendHTTPSFailureError(allErrs)
}
