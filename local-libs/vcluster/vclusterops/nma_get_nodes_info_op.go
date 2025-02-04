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
)

// nmaGetNodesInfoOp get nodes info from the NMA /v1/nodes endpoint.
// The result will be saved into a VCoordinationDatabase object.
type nmaGetNodesInfoOp struct {
	opBase
	dbName               string
	catalogPrefix        string
	ignoreInternalErrors bool // e.g. in scrutinize, continue even if host has issues
	vdb                  *VCoordinationDatabase
}

func makeNMAGetNodesInfoOp(hosts []string,
	dbName, catalogPrefix string,
	ignoreInternalErrors bool,
	vdb *VCoordinationDatabase) nmaGetNodesInfoOp {
	op := nmaGetNodesInfoOp{}
	op.name = "NMAGetNodesInfoOp"
	op.description = "Collect nodes information"
	op.hosts = hosts
	op.dbName = dbName
	op.catalogPrefix = catalogPrefix
	op.ignoreInternalErrors = ignoreInternalErrors
	op.vdb = vdb
	op.vdb.HostNodeMap = makeVHostNodeMap()
	return op
}

func (op *nmaGetNodesInfoOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("nodes")
		httpRequest.QueryParams = map[string]string{"db_name": op.dbName, "catalog_prefix": op.catalogPrefix}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaGetNodesInfoOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaGetNodesInfoOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetNodesInfoOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetNodesInfoOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			var vnode VCoordinationNode
			err := op.parseAndCheckResponse(host, result.content, &vnode)
			if err != nil {
				if op.ignoreInternalErrors {
					op.logger.Error(err, "NMA node info response malformed from host", "Host", host)
					op.logger.PrintWarning("Host %s returned unparsable node info. Skipping host.", host)
				} else {
					return errors.Join(allErrs, err)
				}
			} else {
				vnode.Address = host
				op.vdb.HostNodeMap[host] = &vnode
			}
		} else if result.isInternalError() && op.ignoreInternalErrors {
			op.logger.Error(result.err, "NMA node info reported internal error", "Host", host)
			op.logger.PrintWarning("Host %s reported internal error to node info query. Skipping host.", host)
		} else if result.isTimeout() && op.ignoreInternalErrors {
			// it's unlikely for a node to pass health check but time out here, so leave default timeout limit
			op.logger.PrintWarning("Host %s timed out on node info query. Skipping host.", host)
		} else {
			allErrs = errors.Join(allErrs, result.err)
		}
	}

	return allErrs
}
