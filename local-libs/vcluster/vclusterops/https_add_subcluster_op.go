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

type httpsAddSubclusterOp struct {
	opBase
	opHTTPSBase
	hostRequestBodyMap map[string]string
	scName             string
	isSecondary        bool
	ctlSetSize         int
}

func makeHTTPSAddSubclusterOp(useHTTPPassword bool, userName string, httpsPassword *string,
	scName string, isPrimary bool, ctlSetSize int) (httpsAddSubclusterOp, error) {
	op := httpsAddSubclusterOp{}
	op.name = "HTTPSAddSubclusterOp"
	op.description = "Add subcluster to catalog"
	op.scName = scName
	op.isSecondary = !isPrimary
	op.ctlSetSize = ctlSetSize

	op.useHTTPPassword = useHTTPPassword
	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	return op, nil
}

type addSubclusterRequestData struct {
	IsSecondary bool `json:"is_secondary"`
	CtlSetSize  int  `json:"control_set_size,omitempty"`
}

func (op *httpsAddSubclusterOp) setupRequestBody(hosts []string) error {
	op.hostRequestBodyMap = make(map[string]string)

	for _, host := range hosts {
		addSubclusterData := addSubclusterRequestData{}
		addSubclusterData.IsSecondary = op.isSecondary
		addSubclusterData.CtlSetSize = op.ctlSetSize

		dataBytes, err := json.Marshal(addSubclusterData)
		if err != nil {
			return fmt.Errorf("[%s] fail to marshal request data to JSON string, detail %w", op.name, err)
		}

		op.hostRequestBodyMap[host] = string(dataBytes)
	}

	return nil
}

func (op *httpsAddSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.RequestData = op.hostRequestBodyMap[host]
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsAddSubclusterOp) prepare(execContext *opEngineExecContext) error {
	if len(execContext.upHosts) == 0 {
		return fmt.Errorf(`[%s] Cannot find any main cluster up hosts in OpEngineExecContext`, op.name)
	}
	// use first up host to execute https post request, this host will be the initiator
	hosts := []string{execContext.upHosts[0]}
	err := op.setupRequestBody(hosts)
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(hosts)

	return op.setupClusterHTTPRequest(hosts)
}

func (op *httpsAddSubclusterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsAddSubclusterOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			// skip checking response from other nodes because we will get the same error there
			return result.err
		}
		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			// try processing other hosts' responses when the current host has some server errors
			continue
		}

		// decode the json-format response
		// The successful response object will be a dictionary:
		/*
			{
			  "detail": ""
			}
		*/
		_, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			return fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
		}

		return nil
	}

	return allErrs
}

func (op *httpsAddSubclusterOp) finalize(_ *opEngineExecContext) error {
	return nil
}
