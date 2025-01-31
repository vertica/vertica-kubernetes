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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

type httpsUpdateNodeStateOp struct {
	opBase
	opHTTPSBase
	vdb *VCoordinationDatabase
	// The timeout for each http request. Requests will be repeated if timeout hasn't been exceeded.
	httpRequestTimeout int
}

func makeHTTPSUpdateNodeStateOp(vdb *VCoordinationDatabase,
	useHTTPPassword bool,
	userName string,
	httpsPassword *string,
) (httpsUpdateNodeStateOp, error) {
	op := httpsUpdateNodeStateOp{}
	op.name = "HTTPSUpdateNodeStateOp"
	op.description = "Update node state from running database"
	op.vdb = vdb
	op.useHTTPPassword = useHTTPPassword
	op.httpRequestTimeout = defaultHTTPSRequestTimeoutSeconds

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}

	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

func (op *httpsUpdateNodeStateOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = op.httpRequestTimeout
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + host)
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsUpdateNodeStateOp) prepare(execContext *opEngineExecContext) error {
	op.hosts = op.vdb.HostList
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsUpdateNodeStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsUpdateNodeStateOp) processResult(execContext *opEngineExecContext) error {
	// VER-93706 may update the error handling in this function
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// A host may have precondition failed, such as
		// "Local node has not joined cluster yet, HTTP server will accept connections when the node has joined the cluster"
		// In this case, we mark the node status as UNKNOWN
		if result.hasPreconditionFailed() {
			vnode, ok := op.vdb.HostNodeMap[host]
			if !ok {
				return fmt.Errorf("cannot find host %s in vdb", host)
			}
			// Compute nodes will persistently fail the precondition, and shouldn't have status overwritten.
			// Note that if the vdb was constructed by querying node(s) from a different sandbox than the
			// compute node, it will already have UNKNOWN state in the vdb and that will not change here.
			if vnode.State != util.NodeComputeState {
				vnode.State = util.NodeUnknownState
			}

			continue
		}

		if result.isUnauthorizedRequest() {
			op.logger.PrintError("[%s] unauthorized request: %s", op.name, result.content)
			execContext.hostsWithWrongAuth = append(execContext.hostsWithWrongAuth, host)
			// return here because we assume that
			// we will get the same error across other nodes
			return result.err
		}

		if !result.isPassing() {
			// for failed request, we set the host's state to DOWN
			// only if its current state is UNKNOWN
			vnode, ok := op.vdb.HostNodeMap[host]
			if !ok {
				return fmt.Errorf("cannot find host %s in vdb", host)
			}
			if vnode.State == util.NodeUnknownState {
				vnode.State = util.NodeDownState
			}

			continue
		}

		// parse the /nodes/<host_ip> endpoint response
		nodesInformation := nodesInfo{}
		err := op.parseAndCheckResponse(host, result.content, &nodesInformation)
		if err != nil {
			return fmt.Errorf("[%s] fail to parse result on host %s: %w",
				op.name, host, err)
		}

		if len(nodesInformation.NodeList) == 1 {
			nodeInfo := nodesInformation.NodeList[0]
			vnode, ok := op.vdb.HostNodeMap[host]
			if !ok {
				return fmt.Errorf("cannot find host %s in vdb", host)
			}
			vnode.State = nodeInfo.State
			vnode.IsPrimary = nodeInfo.IsPrimary
		} else {
			// if the result format is wrong on any of the hosts, we should throw an error
			return fmt.Errorf(util.NodeInfoCountMismatch, op.name, len(nodesInformation.NodeList), host)
		}
	}

	return nil
}

func (op *httpsUpdateNodeStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}
