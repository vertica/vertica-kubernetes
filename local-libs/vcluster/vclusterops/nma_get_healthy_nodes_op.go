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
)

// nodes being down is not unusual for the purpose of this op.
// don't block 6 minutes because of one down node.
const healthRequestTimeoutSeconds = 20

type nmaGetHealthyNodesOp struct {
	opBase
	vdb *VCoordinationDatabase
}

func makeNMAGetHealthyNodesOp(hosts []string,
	vdb *VCoordinationDatabase) nmaGetHealthyNodesOp {
	op := nmaGetHealthyNodesOp{}
	op.name = "NMAGetHealthyNodesOp"
	op.description = "Get healthy nodes"
	op.hosts = hosts
	op.vdb = vdb
	return op
}

func (op *nmaGetHealthyNodesOp) setupClusterHTTPRequest(hosts []string) error {
	op.vdb.HostList = []string{}
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.Timeout = healthRequestTimeoutSeconds
		httpRequest.buildNMAEndpoint("health")
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaGetHealthyNodesOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaGetHealthyNodesOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaGetHealthyNodesOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaGetHealthyNodesOp) processResult(_ *opEngineExecContext) error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isPassing() {
			_, err := op.parseAndCheckMapResponse(host, result.content)
			if err == nil {
				op.vdb.HostList = append(op.vdb.HostList, host)
			} else {
				op.logger.Error(err, "NMA health check response malformed from host", "Host", host)
				op.logger.PrintWarning("Skipping unhealthy host %s", host)
			}
		} else {
			op.logger.Error(result.err, "Host is not reachable", "Host", host)
			op.logger.PrintWarning("Skipping unreachable host %s", host)
		}
	}
	if len(op.vdb.HostList) == 0 {
		return fmt.Errorf("NMA is down or unresponsive on all hosts")
	}

	return nil
}
