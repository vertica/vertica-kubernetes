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

type httpsDemoteSubclusterOp struct {
	opBase
	opHTTPSBase
	scName  string
	sandbox string
	vdb     *VCoordinationDatabase
}

func makeHTTPSDemoteSubclusterOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, scName string, sandbox string,
	vdb *VCoordinationDatabase) (httpsDemoteSubclusterOp, error) {
	op := httpsDemoteSubclusterOp{}
	op.name = "HTTPSDemoteSubclusterOp"
	op.hosts = hosts
	op.description = " Demote a subcluster from primary to secondary"
	op.useHTTPPassword = useHTTPPassword
	op.scName = scName
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

func (op *httpsDemoteSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName + "/demote")
		if op.useHTTPPassword {
			httpRequest.Username = op.userName
			httpRequest.Password = op.httpsPassword
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsDemoteSubclusterOp) prepare(execContext *opEngineExecContext) error {
	// If no hosts passed in, we will find the hosts from execute-context
	if len(op.hosts) == 0 {
		upHosts, err := getInitiatorHostInCluster(op.name, op.sandbox, op.scName, op.vdb)
		if err != nil {
			return fmt.Errorf(`[%s] cannot find initial up hosts in the subcluster %s`, op.name, op.scName)
		}
		op.hosts = upHosts
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsDemoteSubclusterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsDemoteSubclusterOp) processResult(_ *opEngineExecContext) error {
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
				"detail": "DEMOTE SUBCLUSTER TO SECONDARY"
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

func (op *httpsDemoteSubclusterOp) finalize(_ *opEngineExecContext) error {
	return nil
}
