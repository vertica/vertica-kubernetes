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

	mapset "github.com/deckarep/golang-set/v2"
)

type nmaCheckClusterVersionOp struct {
	opBase
}

// makeNMACheckClusterVersionOp will be only used after makeNMAVerticaVersionOpBeforeStartNode
// because start_node needs to
// - check unreachable hosts and check version in each subcluster
// - check Vertica version in all hosts of the same sandbox / main cluster
func makeNMACheckClusterVersionOp(hosts []string,
	vdb *VCoordinationDatabase,
	sandboxName string) nmaCheckClusterVersionOp {
	op := nmaCheckClusterVersionOp{}
	op.name = "NMACheckClusterVersionOp"
	op.description = "Check Vertica version in the entire cluster"
	// only check version of the nodes in the same sandbox / main cluster
	for _, h := range hosts {
		if vnode, exist := vdb.HostNodeMap[h]; exist {
			if vnode.Sandbox == sandboxName {
				op.hosts = append(op.hosts, h)
			}
		}
	}

	return op
}

func (op *nmaCheckClusterVersionOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildNMAEndpoint("vertica/version")
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *nmaCheckClusterVersionOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *nmaCheckClusterVersionOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *nmaCheckClusterVersionOp) finalize(_ *opEngineExecContext) error {
	return nil
}

func (op *nmaCheckClusterVersionOp) processResult(_ *opEngineExecContext) error {
	clusterVersion := mapset.NewSet[string]()
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		versionMap, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			return fmt.Errorf("[%s] fail to parse result on host %s, details: %w", op.name, host, err)
		}
		versionStr, ok := versionMap["vertica_version"]
		// missing key "vertica_version"
		if !ok {
			return fmt.Errorf("unable to get vertica version from host %s", host)
		}

		clusterVersion.Add(versionStr)
	}

	if clusterVersion.Cardinality() > 1 {
		return fmt.Errorf("different Vertica versions %+v detected in the cluster hosts", clusterVersion)
	}

	return nil
}
