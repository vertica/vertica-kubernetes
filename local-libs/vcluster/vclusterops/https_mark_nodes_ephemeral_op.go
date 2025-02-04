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
	"strings"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsMarkEphemeralNodeOp struct {
	opBase
	opHTTPSBase
	targetNodeName string
}

func makeHTTPSMarkEphemeralNodeOp(nodeName string,
	initiatorHost []string,
	useHTTPPassword bool,
	userName string,
	httpsPassword *string) (httpsMarkEphemeralNodeOp, error) {
	op := httpsMarkEphemeralNodeOp{}
	op.name = "HTTPSMarkEphemeralNodeOp"
	op.description = "Change node type to ephemeral"
	op.hosts = initiatorHost
	op.targetNodeName = nodeName
	op.useHTTPPassword = useHTTPPassword
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsMarkEphemeralNodeOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + op.targetNodeName + "/ephemeral")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *httpsMarkEphemeralNodeOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsMarkEphemeralNodeOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsMarkEphemeralNodeOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	const errComputeNodeMsg = "cannot change node type from compute to ephemeral"

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isSuccess() {
			if result.isInternalError() {
				errLower := strings.ToLower(result.err.Error())
				if strings.Contains(errLower, errComputeNodeMsg) {
					// down compute nodes can be skipped
					continue
				}
			}
			allErrs = errors.Join(allErrs, result.err)
			continue
		}
	}
	return allErrs
}

func (op *httpsMarkEphemeralNodeOp) finalize(_ *opEngineExecContext) error {
	return nil
}
