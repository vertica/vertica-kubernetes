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

type httpsCreateNodesDepotOp struct {
	opBase
	opHTTPSBase
	HostNodeMap    vHostNodeMap
	DepotSize      string
	InitiatorHosts []string
}

func makeHTTPSCreateNodesDepotOpHelper(vdb *VCoordinationDatabase, newHosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string, initiatorHost []string) (httpsCreateNodesDepotOp, error) {
	op := httpsCreateNodesDepotOp{}
	op.name = "HTTPSCreateNodesDepotOp"
	op.description = "Create depot for new nodes"
	op.hosts = newHosts
	op.useHTTPPassword = useHTTPPassword
	op.HostNodeMap = vdb.HostNodeMap
	op.DepotSize = vdb.DepotSize
	op.InitiatorHosts = initiatorHost

	err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
	if err != nil {
		return op, err
	}

	op.userName = userName
	op.httpsPassword = httpsPassword
	return op, nil
}

// makeHTTPSCreateNodesDepotOp will make an op that call vertica-http service to create depot for the new nodes
func makeHTTPSCreateNodesDepotOp(vdb *VCoordinationDatabase, nodes []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
) (httpsCreateNodesDepotOp, error) {
	var emptyInit []string
	op, err := makeHTTPSCreateNodesDepotOpHelper(vdb, nodes, useHTTPPassword, userName, httpsPassword, emptyInit /*call ops on new hosts*/)

	if err != nil {
		return op, err
	}

	return op, nil
}

func makeHTTPSCreateNodesDepotNoAutoStartOp(vdb *VCoordinationDatabase, hosts, newHosts []string,
	useHTTPPassword bool, userName string, httpsPassword *string,
) (httpsCreateNodesDepotOp, error) {
	op, err := makeHTTPSCreateNodesDepotOpHelper(vdb, newHosts, useHTTPPassword, userName, httpsPassword, hosts)

	if err != nil {
		return op, err
	}

	return op, nil
}

func (op *httpsCreateNodesDepotOp) setupClusterHTTPRequest(hosts []string) error {
	initiatorHost := ""
	if len(op.InitiatorHosts) > 0 {
		initiatorHost = op.InitiatorHosts[0]
	}
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = PostMethod
		node := op.HostNodeMap[host]
		httpRequest.buildHTTPSEndpoint(util.NodesEndpoint + node.Name + "/depot")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		httpRequest.QueryParams = map[string]string{"path": node.DepotPath}
		if op.DepotSize != "" {
			httpRequest.QueryParams["size"] = op.DepotSize
		}
		if initiatorHost != "" {
			op.clusterHTTPRequest.RequestCollection[initiatorHost] = httpRequest
		} else {
			op.clusterHTTPRequest.RequestCollection[host] = httpRequest
		}
	}

	return nil
}

func (op *httpsCreateNodesDepotOp) prepare(execContext *opEngineExecContext) error {
	execHosts := op.hosts
	if len(op.InitiatorHosts) > 0 {
		execHosts = op.InitiatorHosts
	}

	execContext.dispatcher.setup(execHosts)
	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsCreateNodesDepotOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

func (op *httpsCreateNodesDepotOp) processResult(_ *opEngineExecContext) error {
	var allErrs error
	// every host needs to have a successful result, otherwise we fail this op
	// because we want depot created successfully on all hosts
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			allErrs = errors.Join(allErrs, result.err)
			// not break here because we want to log all the failed nodes
			continue
		}

		/* decode the json-format response
		The successful response object will be a dictionary like below:
		{
			"node": "node01",
			"depot_location": "TMPDIR/create_depot/test_db/node01_depot"
		}
		*/
		resp, err := op.parseAndCheckMapResponse(host, result.content)
		if err != nil {
			err = fmt.Errorf(`[%s] fail to parse result on host %s, details: %w`, op.name, host, err)
			allErrs = errors.Join(allErrs, err)
			// not break here because we want to log all the failed nodes
			continue
		}

		// initiator host being empty means that we sent a per-node (new host) request, need to make sure the host matches the node name
		if len(op.InitiatorHosts) == 0 {
			// verify if the node name and the depot location are correct
			if (resp["node"] != op.HostNodeMap[host].Name) || (resp["depot_location"] != op.HostNodeMap[host].DepotPath) {
				err := fmt.Errorf(`[%s] should create depot %s on node %s, but created depot %s on node %s from host %s`,
					op.name, op.HostNodeMap[host].DepotPath, op.HostNodeMap[host].Name, resp["depot_location"], resp["node"], host)
				allErrs = errors.Join(allErrs, err)
				// not break here because we want to log all the failed nodes
			}
		}
	}
	return allErrs
}

func (op *httpsCreateNodesDepotOp) finalize(_ *opEngineExecContext) error {
	return nil
}
