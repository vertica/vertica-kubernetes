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

type httpsConvertSandboxToMainOp struct {
	opBase
	opHTTPSBase
	sandbox string
}

func makeHTTPSConvertSandboxToMainOp(hosts []string, userName string,
	httpsPassword *string, useHTTPPassword bool, sandbox string) (httpsConvertSandboxToMainOp, error) {
	op := httpsConvertSandboxToMainOp{}
	op.name = "HTTPSConvertSandboxToMainOp"
	op.description = "Convert local sandbox to main cluster"
	op.hosts = hosts
	op.sandbox = sandbox
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

func (op *httpsConvertSandboxToMainOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint("sandbox/convert")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsConvertSandboxToMainOp) prepare(execContext *opEngineExecContext) error {
	if len(op.hosts) == 0 {
		return fmt.Errorf("[%s] cannot find any up hosts in the sandbox %s", op.name, op.sandbox)
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsConvertSandboxToMainOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsConvertSandboxToMainOp) processResult(_ *opEngineExecContext) error {
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
			  "detail": "The sandbox has been converted to main successfully."
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

func (op *httpsConvertSandboxToMainOp) finalize(_ *opEngineExecContext) error {
	return nil
}
