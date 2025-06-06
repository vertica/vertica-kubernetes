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
	"errors"
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsSetTLSConfigOp struct {
	opBase
	opHTTPSBase
}

func makeHTTPSSetTLSConfigAuthOp(hosts []string, useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsSetTLSConfigOp, error) {
	op := httpsSetTLSConfigOp{}
	op.name = "HTTPSSetTLSConfigOp"
	op.description = "Initialize client-server TLS from bootstrap config"
	// this op is a cluster-wide op, should be sent to only one host
	op.hosts = hosts
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

func (op *httpsSetTLSConfigOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.TLSBootstrapEndpoint)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsSetTLSConfigOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsSetTLSConfigOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsSetTLSConfigOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	// should only send request to one host as creating authentication method is a cluster-wide op
	// using for-loop here for accommodating potential future cases for sandboxes
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)
		err := result.getError(host, op.name)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// Example successful response object:
		/*
			{
			  "detail": "INITIALIZED CLIENT-SERVER TLS"
			}
		*/
		_, err = op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			return fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
		}
		return nil
	}

	return allErrs
}

func (op *httpsSetTLSConfigOp) finalize(_ *opEngineExecContext) error {
	return nil
}
