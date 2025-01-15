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

const startupOp = "startupOp"
const generalStartNodeDesc = "Get Vertica startup command"
const startNodeAfterUnsandboxDesc = "Get Vertica startup command for unsandboxed nodes"

type httpsStartUpCommandOp struct {
	opBase
	opHTTPSBase
	vdb     *VCoordinationDatabase
	cmdType CmdType
	sandbox string
}

func makeHTTPSStartUpCommandOp(useHTTPPassword bool, userName string, httpsPassword *string,
	vdb *VCoordinationDatabase) (httpsStartUpCommandOp, error) {
	op := httpsStartUpCommandOp{}
	op.name = startupOp
	op.description = generalStartNodeDesc
	op.useHTTPPassword = useHTTPPassword
	op.vdb = vdb
	op.sandbox = util.MainClusterSandbox

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

func makeHTTPSStartUpCommandOpAfterUnsandbox(useHTTPPassword bool, userName string,
	httpsPassword *string) (httpsStartUpCommandOp, error) {
	op := httpsStartUpCommandOp{}
	op.name = startupOp
	op.description = startNodeAfterUnsandboxDesc
	op.useHTTPPassword = useHTTPPassword
	op.cmdType = UnsandboxSCCmd
	op.sandbox = util.MainClusterSandbox

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

// Use the response from an UP host of the specified sandbox
func makeHTTPSStartUpCommandWithSandboxOp(useHTTPPassword bool, userName string, httpsPassword *string,
	vdb *VCoordinationDatabase, sandbox string) (httpsStartUpCommandOp, error) {
	op := httpsStartUpCommandOp{}
	op.name = startupOp
	op.description = generalStartNodeDesc
	op.useHTTPPassword = useHTTPPassword
	op.vdb = vdb
	op.sandbox = sandbox

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

func (op *httpsStartUpCommandOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("startup/commands")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsStartUpCommandOp) prepare(execContext *opEngineExecContext) error {
	// Use the /v1/startup/command endpoint for a primary Up host to view every start command of existing nodes
	// With sandboxes in a cluster, we need to ensure that we pick a main cluster UP host
	if op.cmdType == UnsandboxSCCmd {
		for h, sb := range execContext.upHostsToSandboxes {
			if sb == "" {
				op.hosts = append(op.hosts, h)
				break
			}
		}
	} else {
		var primaryUpHosts []string
		var upHosts []string
		for host, vnode := range op.vdb.HostNodeMap {
			// If we do not find a primary up host in the same cluster(or sandbox), try to find a secondary up host
			if vnode.State == util.NodeUpState && vnode.Sandbox == op.sandbox {
				if vnode.IsPrimary {
					primaryUpHosts = append(primaryUpHosts, host)
					break
				}
				upHosts = append(upHosts, host)
			}
		}
		if len(primaryUpHosts) > 0 {
			op.hosts = primaryUpHosts
		} else {
			op.logger.Info("could not find any primary UP nodes, considering secondary UP nodes.")
			op.hosts = []string{upHosts[0]}
		}
	}
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsStartUpCommandOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsStartUpCommandOp) processResult(execContext *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return fmt.Errorf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
		}

		if result.isPassing() {
			type HTTPStartUpCommandResponse map[string][]string
			/* "v_practice_db_node0001": [
				  "\/opt\/vertica\/bin\/vertica",
				  "-D",
				  "\/data\/practice_db\/v_practice_db_node0001_catalog",
				  "-C",
			 	  "practice_db",
				  "-n",
				  "v_practice_db_node0001",
				  "-h",
				  "192.168.1.101",
				  "-p",
				  "5433",
				  "-P",
				  "4803",
				  "-Y",
				  "ipv4"
				],
				"v_practice_db_node0002": [
				  "\/opt\/vertica\/bin\/vertica",
				  "-D",
				  "\/data\/practice_db\/v_practice_db_node0002_catalog",
				  "-C",
				  "practice_db",
				  "-n",
				  "v_practice_db_node0002",
				  "-h",
				  "192.168.1.102",
				  "-p",
				  "5433",
				  "-P",
				  "4803",
				  "-Y",
				  "ipv4"
			    ],
			*/
			var responseObj HTTPStartUpCommandResponse
			err := op.parseAndCheckResponse(host, result.content, &responseObj)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				continue
			}
			execContext.startupCommandMap = responseObj
			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return nil
}

func (op *httpsStartUpCommandOp) finalize(_ *opEngineExecContext) error {
	return nil
}
