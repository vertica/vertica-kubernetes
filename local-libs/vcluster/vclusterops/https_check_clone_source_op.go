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
	"fmt"

	"github.com/vertica/vcluster/vclusterops/util"
)

// httpsCheckCloneSourceOp verifies that the source subcluster has the expected
// number of nodes for cloning. It queries the /nodes endpoint to get accurate node counts.
type httpsCheckCloneSourceOp struct {
	opBase
	opHTTPSBase
	sourceSubcluster string
	requiredNodes    int
}

func makeHTTPSCheckCloneSourceOp(useHTTPPassword bool, userName string,
	httpsPassword *string, sourceSC string, targetNodeCount int) (httpsCheckCloneSourceOp, error) {
	op := httpsCheckCloneSourceOp{}
	op.name = "HTTPSCheckCloneSourceOp"
	op.description = "Check clone source subcluster node count"
	op.sourceSubcluster = sourceSC
	op.requiredNodes = targetNodeCount

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

func (op *httpsCheckCloneSourceOp) setupClusterHTTPRequest(hosts []string) error {
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

func (op *httpsCheckCloneSourceOp) prepare(execContext *opEngineExecContext) error {
	if len(execContext.upHosts) == 0 {
		return fmt.Errorf("[%s] no up hosts available in execution context", op.name)
	}
	hosts := []string{execContext.upHosts[0]}
	execContext.dispatcher.setup(hosts)
	return op.setupClusterHTTPRequest(hosts)
}

func (op *httpsCheckCloneSourceOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}
	return op.processResult(execContext)
}

func (op *httpsCheckCloneSourceOp) processResult(_ *opEngineExecContext) error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if result.isUnauthorizedRequest() {
			return result.err
		}

		if !result.isPassing() {
			return fmt.Errorf("[%s] failed to query nodes from host %s: %w",
				op.name, host, result.err)
		}

		// Parse nodes response
		nodesInfo := nodesStateInfo{}
		err := op.parseAndCheckResponse(host, result.content, &nodesInfo)
		if err != nil {
			return fmt.Errorf("[%s] failed to parse nodes response from %s: %w",
				op.name, host, err)
		}

		// Count nodes in source subcluster
		sourceNodeCount := 0
		for i := range nodesInfo.NodeList {
			if nodesInfo.NodeList[i].Subcluster == op.sourceSubcluster {
				sourceNodeCount++
				op.logger.PrintInfo("[%s] found node %s in source subcluster '%s'",
					op.name, nodesInfo.NodeList[i].Name, op.sourceSubcluster)
			}
		}

		op.logger.PrintInfo("[%s] source subcluster '%s' has %d node(s)",
			op.name, op.sourceSubcluster, sourceNodeCount)

		// Validate node count matches
		if sourceNodeCount != op.requiredNodes {
			return fmt.Errorf("[%s] node count mismatch: source subcluster '%s' "+
				"has %d node(s), but target will have %d node(s). "+
				"Both subclusters must have equal node counts when using --like",
				op.name, op.sourceSubcluster, sourceNodeCount, op.requiredNodes)
		}

		op.logger.PrintInfo("[%s] validated source subcluster '%s' with %d nodes matching target",
			op.name, op.sourceSubcluster, sourceNodeCount)
		return nil
	}

	return fmt.Errorf("[%s] no successful response received", op.name)
}

func (op *httpsCheckCloneSourceOp) finalize(_ *opEngineExecContext) error {
	return nil
}
