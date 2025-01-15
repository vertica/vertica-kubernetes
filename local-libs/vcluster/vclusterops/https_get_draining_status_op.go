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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsGetDrainingStatusOp struct {
	opBase
	opHTTPSBase
	sandbox string
	dsList  *DrainingStatusList
}

func makeHTTPSGetDrainingStatusOp(useHTTPPassword bool, sandbox, userName string,
	httpsPassword *string, drainingStatusList *DrainingStatusList) (httpsGetDrainingStatusOp, error) {
	op := httpsGetDrainingStatusOp{}
	op.name = "HTTPSGetDrainingStatusOp"
	op.description = "Get draining status"
	op.useHTTPPassword = useHTTPPassword
	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	op.sandbox = sandbox
	op.dsList = drainingStatusList
	return op, nil
}

func (op *httpsGetDrainingStatusOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("subcluster/draining-status")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetDrainingStatusOp) prepare(execContext *opEngineExecContext) error {
	if len(execContext.upHostsToSandboxes) == 0 {
		return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
	}
	// pick up hosts in the sandbox to send the https request
	for host, sb := range execContext.upHostsToSandboxes {
		if sb == op.sandbox {
			op.hosts = append(op.hosts, host)
		}
	}
	if len(op.hosts) == 0 {
		return fmt.Errorf(`[%s] Cannot find any up hosts in %s`, op.name, util.GetClusterName(op.sandbox))
	}

	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetDrainingStatusOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsGetDrainingStatusOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}

		if result.isPassing() {
			// decode the json-format response
			// The successful response will contain all subclusters' draining status:
			/*
				{
				  "draining_status_list": [
				    {
				      "subcluster_name": "default_subcluster",
				      "drain_status": "pausing",
				      "redirect_to": ""
				    }
				  ]
				}
			*/
			resp := DrainingStatusList{}
			err := op.parseAndCheckResponse(host, result.content, &resp)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}

			// collect draining status
			*op.dsList = resp
			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return appendHTTPSFailureError(allErrs)
}

func (op *httpsGetDrainingStatusOp) finalize(_ *opEngineExecContext) error {
	return nil
}
