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

type httpsRenameSubclusterOp struct {
	opBase
	scName    string
	newSCName string
	sandbox   string
	vdb       *VCoordinationDatabase
	opHTTPSBase
}

func makeHTTPSRenameSubclusterOp(hosts []string, useHTTPPassword bool,
	userName string, httpsPassword *string, scName, newSCName, sandbox string,
	vdb *VCoordinationDatabase) (httpsRenameSubclusterOp, error) {
	op := httpsRenameSubclusterOp{}
	op.name = "HTTPSRenameSubclusterOp"
	op.description = "Rename a subcluster"
	op.hosts = hosts
	op.scName = scName
	op.newSCName = newSCName
	op.sandbox = sandbox
	op.vdb = vdb
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

func (op *httpsRenameSubclusterOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PutMethod
		httpRequest.buildHTTPSEndpoint(util.SubclustersEndpoint + op.scName + "/rename")
		httpRequest.QueryParams = make(map[string]string)
		httpRequest.QueryParams["name"] = op.newSCName

		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsRenameSubclusterOp) prepare(execContext *opEngineExecContext) error {
	// If no hosts passed in, we will find the hosts from execute-context
	if len(op.hosts) == 0 {
		// Find the first up hosts from the main cluster and sandbox
		mainUpHost, sandboxUpHost := op.findUpHostForMainAndSandbox(op.vdb)

		// If a sandbox is specified, use two up hosts: one from the main cluster and one from the sandbox
		// to execute https put request. Otherwise, use only the up host from the main cluster.
		var upHosts []string
		if mainUpHost != "" {
			upHosts = append(upHosts, mainUpHost)
		} else {
			return fmt.Errorf(`[%s] cannot find any up host in main cluster`, op.name)
		}
		if op.sandbox != "" && sandboxUpHost != "" {
			upHosts = append(upHosts, sandboxUpHost)
		} else if op.sandbox != "" {
			return fmt.Errorf(`[%s] cannot find any up host in sandbox %s`, op.name, op.sandbox)
		}
		op.hosts = upHosts
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsRenameSubclusterOp) findUpHostForMainAndSandbox(vdb *VCoordinationDatabase) (mainUpHost, sandboxUpHost string) {
	for _, node := range vdb.HostNodeMap {
		if node.State == util.NodeDownState {
			continue
		}
		if node.Sandbox == "" && mainUpHost == "" {
			mainUpHost = node.Address
		}
		if node.Sandbox == op.sandbox && sandboxUpHost == "" && node.Sandbox != "" {
			sandboxUpHost = node.Address
		}
		if mainUpHost != "" && (sandboxUpHost != "" || op.sandbox == "") {
			break
		}
	}
	return mainUpHost, sandboxUpHost
}

func (op *httpsRenameSubclusterOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsRenameSubclusterOp) processResult(_ *opEngineExecContext) error {
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

func (op *httpsRenameSubclusterOp) finalize(_ *opEngineExecContext) error {
	return nil
}
