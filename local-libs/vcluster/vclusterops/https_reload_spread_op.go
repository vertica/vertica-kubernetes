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

type httpsReloadSpreadOp struct {
	opBase
	opHTTPSBase
}

func makeHTTPSReloadSpreadOpWithInitiator(initHosts []string,
	useHTTPPassword bool,
	userName string, httpsPassword *string) (httpsReloadSpreadOp, error) {
	op := httpsReloadSpreadOp{}
	op.name = "HTTPSReloadSpreadOp"
	op.description = "Reload spread"
	op.hosts = initHosts
	op.useHTTPPassword = useHTTPPassword

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func makeHTTPSReloadSpreadOp(useHTTPPassword bool,
	userName string, httpsPassword *string) (httpsReloadSpreadOp, error) {
	return makeHTTPSReloadSpreadOpWithInitiator(nil, useHTTPPassword, userName, httpsPassword)
}

func (op *httpsReloadSpreadOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint("config/spread/reload")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsReloadSpreadOp) prepare(execContext *opEngineExecContext) error {
	// If the host input is an empty string, we find up hosts to update the host input
	if len(op.hosts) == 0 {
		op.hosts = execContext.upHosts
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsReloadSpreadOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsReloadSpreadOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}

		// decode the json-format response
		// The successful response object will be a dictionary as below:
		// {"detail": "Reloaded"}
		reloadSpreadRsp, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			continue
		}

		// verify if the response's content is correct
		if reloadSpreadRsp["detail"] != "Reloaded" {
			err = fmt.Errorf(`[%s] response detail should be 'Reloaded' but got '%s'`, op.name, reloadSpreadRsp["detail"])
			allErrs = errors.Join(allErrs, err)
		}
	}

	return allErrs
}

func (op *httpsReloadSpreadOp) finalize(_ *opEngineExecContext) error {
	return nil
}
