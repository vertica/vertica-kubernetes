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

	"github.com/vertica/vcluster/rfc7807"
	"github.com/vertica/vcluster/vclusterops/util"
)

const (
	AnySandbox = "*"
)

type httpsGetNodesInfoOp struct {
	opBase
	opHTTPSBase
	dbName                  string
	vdb                     *VCoordinationDatabase
	allowUseSandboxResponse bool
	sandbox                 string
}

func makeHTTPSGetNodesInfoOp(dbName string, hosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string, vdb *VCoordinationDatabase,
	allowUseSandboxResponse bool, sandbox string) (httpsGetNodesInfoOp, error) {
	op := httpsGetNodesInfoOp{}
	op.name = "HTTPSGetNodeInfoOp"
	op.description = "Collect node information"
	op.dbName = dbName
	op.hosts = hosts
	op.vdb = vdb

	err := op.validateAndSetUsernameAndPassword(op.name, useHTTPPassword, userName,
		httpsPassword)

	op.allowUseSandboxResponse = allowUseSandboxResponse
	op.sandbox = sandbox
	return op, err
}

func (op *httpsGetNodesInfoOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("nodes")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}

		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetNodesInfoOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetNodesInfoOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsGetNodesInfoOp) shouldUseResponse(host string, nodesStates *nodesStateInfo) bool {
	// should use a response from a non-sandboxed node to build vdb in most cases, i.e.,
	// create db, remove node, remove subcluster, add node and unsandbox;
	// there is one case that don't care about where the response is from: start node
	responseSandbox := ""
	for _, node := range nodesStates.NodeList {
		if node.Address == host {
			responseSandbox = node.Sandbox
			break
		}
	}
	// continue to parse next response if a response from main cluster node is expected
	if responseSandbox != "" && !op.allowUseSandboxResponse {
		return false
	}

	// continue to parse next response if a response from a different sandbox is expected
	if op.sandbox != AnySandbox && responseSandbox != op.sandbox && op.sandbox != util.MainClusterSandbox {
		return false
	}
	return true
}

func (op *httpsGetNodesInfoOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		// A host may have precondition failed, such as
		// "Local node has not joined cluster yet, HTTP server will accept connections when the node has joined the cluster"
		// In this case, we skip use the information from that host
		if result.hasPreconditionFailed() {
			continue
		}

		if result.isUnauthorizedRequest() {
			detail := fmt.Sprintf("[%s] wrong password/certificate for https service on host %s",
				op.name, host)
			return rfc7807.New(rfc7807.AuthenticationError).WithHost(host).WithDetail(detail)
		}

		if result.isPassing() {
			// parse the /nodes endpoint response
			nodesStates := nodesStateInfo{}
			err := op.parseAndCheckResponse(host, result.content, &nodesStates)
			if err != nil {
				allErrs = errors.Join(allErrs, err)
				break
			}
			if !op.shouldUseResponse(host, &nodesStates) {
				continue
			}
			// save nodes info to vdb
			op.vdb.HostNodeMap = makeVHostNodeMap()
			op.vdb.HostList = []string{}
			op.vdb.PrimaryUpNodes = []string{}
			op.vdb.ComputeNodes = []string{}
			op.vdb.UnboundNodes = []*VCoordinationNode{}
			for _, node := range nodesStates.NodeList {
				if node.Database != op.dbName {
					err = fmt.Errorf(`[%s] database %s is running on host %s, rather than database %s`, op.name, node.Database, host, op.dbName)
					allErrs = errors.Join(allErrs, err)
					return appendHTTPSFailureError(allErrs)
				}
				vnode := buildVnodeFromNodeStateInfo(node)
				if node.IsPrimary && node.State == util.NodeUpState {
					op.vdb.PrimaryUpNodes = append(op.vdb.PrimaryUpNodes, node.Address)
				} else if node.State == util.NodeComputeState {
					op.vdb.ComputeNodes = append(op.vdb.ComputeNodes, node.Address)
				}
				err := op.vdb.addNode(&vnode)
				if err != nil {
					allErrs = errors.Join(allErrs, err)
					return appendHTTPSFailureError(allErrs)
				}
				// extract catalog prefix from node's catalog path
				catalogPrefix, found := extractCatalogPrefix(node.CatalogPath, node.Database, node.Name)
				if !found {
					op.logger.PrintError("[%s] failed to retrieve catalog prefix because catalog path %q is malformed",
						op.name, node.CatalogPath)
				}
				op.vdb.CatalogPrefix = catalogPrefix
			}

			return nil
		}
		allErrs = errors.Join(allErrs, result.err)
	}
	return appendHTTPSFailureError(allErrs)
}

func (op *httpsGetNodesInfoOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func buildVnodeFromNodeStateInfo(node *nodeStateInfo) VCoordinationNode {
	vnode := makeVCoordinationNode()
	vnode.Name = node.Name
	vnode.Address = node.Address
	vnode.CatalogPath = node.CatalogPath
	vnode.DepotPath = node.DepotPath
	vnode.StorageLocations = node.StorageLocations
	vnode.IsPrimary = node.IsPrimary
	vnode.State = node.State
	vnode.Subcluster = node.Subcluster
	vnode.Sandbox = node.Sandbox
	vnode.IsControlNode = node.IsControlNode
	vnode.ControlNode = node.ControlNode

	return vnode
}
