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

type httpsGetLocalNodeStateOp struct {
	opBase
	opHTTPSBase
	dbName               string
	hostsWithNodeDetails hostNodeDetailsMap
}

func makeHTTPSGetLocalNodeStateOp(dbName string, hosts []string, useHTTPPassword bool, userName string,
	httpsPassword *string, hostsWithNodeDetails hostNodeDetailsMap) (httpsGetLocalNodeStateOp, error) {
	op := httpsGetLocalNodeStateOp{}
	op.name = "HTTPSGetLocalNodeStateOp"
	op.description = "Get local node state"
	op.useHTTPPassword = useHTTPPassword
	if useHTTPPassword {
		err := util.ValidateUsernameAndPassword(op.name, useHTTPPassword, userName)
		if err != nil {
			return op, err
		}
		op.userName = userName
		op.httpsPassword = httpsPassword
	}
	op.dbName = dbName
	op.hosts = hosts
	op.hostsWithNodeDetails = hostsWithNodeDetails
	return op, nil
}

func (op *httpsGetLocalNodeStateOp) setupClusterHTTPRequest(hosts []string) error {
	for _, host := range hosts {
		httpRequest := hostHTTPRequest{}
		httpRequest.Method = GetMethod
		httpRequest.buildHTTPSEndpoint("node")
		if op.useHTTPPassword {
			httpRequest.Password = op.httpsPassword
			httpRequest.Username = op.userName
		}
		op.clusterHTTPRequest.RequestCollection[host] = httpRequest
	}

	return nil
}

func (op *httpsGetLocalNodeStateOp) prepare(execContext *opEngineExecContext) error {
	execContext.dispatcher.setup(op.hosts)

	return op.setupClusterHTTPRequest(op.hosts)
}

func (op *httpsGetLocalNodeStateOp) execute(execContext *opEngineExecContext) error {
	if err := op.runExecute(execContext); err != nil {
		return err
	}

	return op.processResult(execContext)
}

type nodeStateResp struct {
	NodeStates []NodeState `json:"node_list"`
}

func (op *httpsGetLocalNodeStateOp) processResult(_ *opEngineExecContext) error {
	for host, result := range op.clusterHTTPRequest.ResultCollection {
		op.logResponse(host, result)

		if !result.isPassing() {
			// we need to collect all nodes info, if one host failed to collect the info,
			// we consider the operation failed.
			return result.err
		}

		// decode the json-format response
		// The successful response will contain one node's info:
		/*
			{
			  "detail": null,
			  "node_list": [
				{
				  "name": "v_test_db_node0001",
				  "node_id": 45035996273704992,
				  "address": "192.168.1.101",
				  "state": "UP",
				  "database": "test_db",
				  "is_primary": true,
				  "is_readonly": false,
				  "catalog_path": "\/data\/test_db\/v_test_db_node0001_catalog\/Catalog",
				  "data_path": [
					"\/data\/test_db\/v_test_db_node0001_data"
				  ],
				  "depot_path": "\/data\/test_db\/v_test_db_node0001_depot",
				  "subcluster_id": 45035996273704988,
				  "subcluster_name": "default_subcluster",
				  "last_msg_from_node_at": "2024-04-05T12:33:19.975952-04",
				  "down_since": null,
				  "build_info": "v24.3.0-a0efe9ba3abb08d9e6472ffc29c8e0949b5998d2",
				  "sandbox_name": "",
				  "number_shard_subscriptions": 3
				}
			  ]
			}
		*/
		resp := nodeStateResp{}
		err := op.parseAndCheckResponse(host, result.content, &resp)
		if err != nil {
			return fmt.Errorf(`[%s] failed to parse result on host %s, details: %w`, op.name, host, err)
		}

		// verify if the endpoint returns correct node info
		if len(resp.NodeStates) != 1 {
			return fmt.Errorf(`[%s] response from host %s should contain state for one node rather than %d node(s)`,
				op.name, host, len(resp.NodeStates))
		}
		nodeState := resp.NodeStates[0]
		if nodeState.Database != op.dbName {
			return fmt.Errorf(`[%s] node is running in database %s rather than database %s on host %s`,
				op.name, nodeState.Database, op.dbName, host)
		}
		if nodeState.Address != host {
			return fmt.Errorf(`[%s] node state is for host %s rather than host %s`,
				op.name, nodeState.Address, host)
		}

		// collect node state
		nodeDetails := NodeDetails{}
		nodeDetails.NodeState = nodeState
		op.hostsWithNodeDetails[host] = &nodeDetails
	}

	return nil
}

func (op *httpsGetLocalNodeStateOp) finalize(_ *opEngineExecContext) error {
	return nil
}
