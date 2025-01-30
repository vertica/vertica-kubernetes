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

type httpsDisallowMultipleNamespacesOp struct {
	opBase
	opHTTPSBase
	sandbox string
	vdb     *VCoordinationDatabase
}

func makeHTTPSDisallowMultipleNamespacesOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, sandbox string, vdb *VCoordinationDatabase) (httpsDisallowMultipleNamespacesOp, error) {
	op := httpsDisallowMultipleNamespacesOp{}
	op.name = "HTTPSGetNamespaceOp"
	op.description = "Get information about all namespaces"
	op.hosts = hosts
	op.useHTTPPassword = useHTTPPassword
	op.sandbox = sandbox
	op.vdb = vdb

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

func (op *httpsDisallowMultipleNamespacesOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("namespaces")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsDisallowMultipleNamespacesOp) prepare(execContext *opEngineExecContext) error {
	sourceHost, err := getInitiatorHostForReplication(op.name, op.sandbox, op.hosts, op.vdb)
	if err != nil {
		return err
	}
	// hosts to be used to make the request should be an up host from source database or sandbox
	op.hosts = sourceHost
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsDisallowMultipleNamespacesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

type namespace struct {
	NamespaceID       int    `json:"namespace_id"`
	NamespaceName     string `json:"namespace_name"`
	IsDefault         bool   `json:"is_default"`
	DefaultShardCount int    `json:"default_shard_count"`
}

type namespaceListResponse struct {
	NamespaceList []namespace `json:"namespace_list"`
}

func (op *httpsDisallowMultipleNamespacesOp) processResult(_ *opEngineExecContext) error {
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
				"namespace_list": [
			    	{
					"namespace_id": NAMESPACE_ID,
					"namespace_name": "default_namespace",
					"is_default": true,
					"default_shard_count": 4
			    	},
					{
					"namespace_id": NAMESPACE_ID,
					"namespace_name": "test_ns",
					"is_default": false,
					"default_shard_count": 6
					}
			  	]
			}
		*/
		namespaceResponse := namespaceListResponse{}
		err := op.parseAndCheckResponse(host, result.content, &namespaceResponse)
		if err != nil {
			allErrs = errors.Join(allErrs, err)
			continue
		}
		// check if the response contains multiple namespaces
		if len(namespaceResponse.NamespaceList) > 1 {
			err := fmt.Errorf("[%s] replication is not supported in multiple namespace database", op.name)
			allErrs = errors.Join(allErrs, err)
			return allErrs
		}
		return nil
	}

	return allErrs
}

func (op *httpsDisallowMultipleNamespacesOp) finalize(_ *opEngineExecContext) error {
	return nil
}
