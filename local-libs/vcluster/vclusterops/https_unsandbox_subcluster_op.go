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

	mapset "github.com/deckarep/golang-set/v2"
	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsUnsandboxingOp struct {
	opBase
	opHTTPSBase
	hostRequestBodyMap map[string]string
	scName             string
	scHosts            *[]string
}

// This op is used to unsandbox the given subcluster `scName`
func makeHTTPSUnsandboxingOp(scName string,
	useHTTPPassword bool, userName string, httpsPassword *string, hosts *[]string) (httpsUnsandboxingOp, error) {
	op := httpsUnsandboxingOp{}
	op.name = "HTTPSUnsansboxingOp"
	op.description = "Convert sandboxed subcluster into regular subcluster in catalog"
	op.useHTTPPassword = useHTTPPassword
	op.scName = scName
	op.scHosts = hosts

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

func (op *httpsUnsandboxingOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName + "/unsandbox")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = op.hostRequestBodyMap
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsUnsandboxingOp) setupRequestBody() error {
	op.hostRequestBodyMap = make(map[string]string)
	return nil
}

func (op *httpsUnsandboxingOp) prepare(execContext *opEngineExecContext) error {
	sandboxes := mapset.NewSet[string]()
	var mainHost string
	if len(execContext.upHostsToSandboxes) == 0 {
		return fmt.Errorf(`[%s] Cannot find any up hosts in OpEngineExecContext`, op.name)
	}
	// use an UP host in main cluster and UP host in separate sc in same sandbox to execute the https post request
	for h, sb := range execContext.upHostsToSandboxes {
		if !sandboxes.Contains(sb) {
			op.hosts = append(op.hosts, h)
			sandboxes.Add(sb)
		}
		if sb == "" {
			mainHost = h
		}
	}
	if mainHost == "" {
		return fmt.Errorf(`[%s] Cannot find any up hosts of main cluster in OpEngineExecContext`, op.name)
	}
	err := op.setupRequestBody()
	if err != nil {
		return err
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsUnsandboxingOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsUnsandboxingOp) processResult(_ *opEngineExecContext) error {
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
			  "detail": "Subcluster 'sc-name' has been unsandboxed. If wiped out and restarted, it should be able to rejoin the cluster."
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

func (op *httpsUnsandboxingOp) finalize(execContext *opEngineExecContext) error {
	*op.scHosts = []string{}
	for _, vnode := range execContext.scNodesInfo {
		*op.scHosts = append(*op.scHosts, vnode.Address)
	}
	return nil
}
