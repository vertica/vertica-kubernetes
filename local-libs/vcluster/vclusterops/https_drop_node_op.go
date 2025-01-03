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
	"strconv"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsDropNodeOp struct {
	opBase
	opHTTPSBase
	targetHost    string
	RequestParams map[string]string
}

// makeHTTPSDropNodeOp is a constructor for httpsDropNodeOp. The cascade option
// should be true if an Eon deployment and the node we are dropping is down.
func makeHTTPSDropNodeOp(vnode string,
	initiatorHost []string,
	useHTTPPassword bool,
	userName string,
	httpsPassword *string,
	cascade bool) (httpsDropNodeOp, error) {
	op := httpsDropNodeOp{}
	op.name = "HTTPSDropNodeOp"
	op.description = "Drop node in catalog"
	op.hosts = initiatorHost
	op.targetHost = vnode
	op.useHTTPPassword = useHTTPPassword
	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}
	op.userName = userName
	op.httpsPassword = httpsPassword
	op.RequestParams = make(map[string]string)
	op.RequestParams["cascade"] = strconv.FormatBool(cascade)
	return op, nil
}

func (op *httpsDropNodeOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + op.targetHost + util.DropEndpoint)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = op.RequestParams
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}
	return nil
}

func (op *httpsDropNodeOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsDropNodeOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsDropNodeOp) processResult(_ *opEngineExecContext) error {
	var allErrs error

	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isSuccess() {
			allErrs = errors.Join(allErrs, result.err)
			continue
		}
	}
	return allErrs
}

func (op *httpsDropNodeOp) finalize(_ *opEngineExecContext) error {
	return nil
}
